package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/kenissha/DevPlatform/backend/internal/access"
	"github.com/kenissha/DevPlatform/backend/internal/audit"
	"github.com/kenissha/DevPlatform/backend/internal/auth"
	"github.com/kenissha/DevPlatform/backend/internal/backup"
	"github.com/kenissha/DevPlatform/backend/internal/config"
	"github.com/kenissha/DevPlatform/backend/internal/deploy"
	"github.com/kenissha/DevPlatform/backend/internal/deployment"
	"github.com/kenissha/DevPlatform/backend/internal/gitauth"
	"github.com/kenissha/DevPlatform/backend/internal/gitserver"
	"github.com/kenissha/DevPlatform/backend/internal/gitstats"
	"github.com/kenissha/DevPlatform/backend/internal/mergerequest"
	"github.com/kenissha/DevPlatform/backend/internal/notify"
	"github.com/kenissha/DevPlatform/backend/internal/repoapi"
	"github.com/kenissha/DevPlatform/backend/internal/repostore"
	"github.com/kenissha/DevPlatform/backend/internal/secretsvault"
	"github.com/kenissha/DevPlatform/backend/internal/server"
	"github.com/kenissha/DevPlatform/backend/internal/taskboard"
	"github.com/kenissha/DevPlatform/backend/internal/users"
)

func main() {
	cfg := config.Load()

	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		log.Fatalf("failed to create data dir %s: %v", cfg.DataDir, err)
	}

	store := repostore.New(cfg.DataDir)
	repos, err := store.List()
	if err != nil {
		log.Fatalf("failed to list repositories: %v", err)
	}
	log.Printf("repository store ready at %s (%d repos)", cfg.DataDir, len(repos))

	gitHandler := gitserver.NewHandler(cfg.DataDir)
	authedGitHandler := gitauth.RequireBasicAuth(cfg.GitUsername, cfg.GitPassword, gitHandler)
	jwtSecret := []byte(cfg.JWTSecret)
	authMiddleware := func(next http.Handler) http.Handler {
		return auth.RequireAuth(jwtSecret, next)
	}
	auditLogger := audit.New(filepath.Join(cfg.DataDir, "audit.jsonl"))
	notifyStore := notify.NewStore(filepath.Join(cfg.DataDir, "notifications"))
	usersStore := users.NewStore(filepath.Join(cfg.DataDir, "users.json"))
	// accessStore starts with nobody restricted — every repo stays visible
	// to everyone exactly as before this existed, until an admin calls
	// PUT /api/access/{subject} for a specific person (see
	// internal/access's doc comment for why unrestricted is the default).
	accessStore := access.NewStore(filepath.Join(cfg.DataDir, "access.json"))

	// SMTPHost is the switch: empty means NoopEmailSender-equivalent
	// behavior (notifications stay panel-only), exactly as before this was
	// wired up at all. Set DEVPLATFORM_SMTP_HOST to turn real mail on.
	if cfg.SMTPHost != "" {
		notifyStore.Sender = &notify.SMTPEmailSender{
			Host:     cfg.SMTPHost,
			Port:     cfg.SMTPPort,
			Username: cfg.SMTPUsername,
			Password: cfg.SMTPPassword,
			From:     cfg.SMTPFrom,
		}
		notifyStore.LookupEmail = func(subject string) (string, bool) {
			u, ok, err := usersStore.Get(subject)
			if err != nil || !ok {
				return "", false
			}
			return u.Email, true
		}
		notifyStore.BaseURL = cfg.BaseURL
		log.Printf("sending real email notifications via %s:%s", cfg.SMTPHost, cfg.SMTPPort)
	}
	mrHandlers := &mergerequest.Handlers{
		Store:  mergerequest.NewStore(filepath.Join(cfg.DataDir, "merge-requests")),
		Repos:  store,
		Audit:  auditLogger,
		Notify: notifyStore,
		Users:  usersStore,
		Access: accessStore,
	}
	repoHandlers := &repoapi.Handlers{Repos: store, Audit: auditLogger, Access: accessStore}
	taskHandlers := &taskboard.Handlers{
		Store:  taskboard.NewStore(filepath.Join(cfg.DataDir, "tasks")),
		Repos:  store,
		Audit:  auditLogger,
		Notify: notifyStore,
		Access: accessStore,
	}
	statsHandlers := &gitstats.Handlers{Repos: store}
	auditHandlers := &audit.Handlers{Logger: auditLogger, Access: accessStore}
	notifyHandlers := &notify.Handlers{
		Store: notifyStore,
	}

	targets, err := deployment.LoadTargets(cfg.DeployTargetsFile)
	if err != nil {
		log.Fatalf("failed to load deploy targets from %q: %v", cfg.DeployTargetsFile, err)
	}
	if cfg.DeployTargetsFile == "" {
		log.Printf("no DEVPLATFORM_DEPLOY_TARGETS_FILE configured — deploy requests can be opened but never approved until one is set")
	}

	// A missing/invalid secrets key is not fatal: it only means deploy
	// targets that ask for a secretsTarget will fail at approval time with
	// a clear error (see deploy.Pipeline.Deploy), the same "misconfigured,
	// not crashed" posture LoadTargets' empty-path case already takes.
	var secretsStore *secretsvault.Store
	if key, err := secretsvault.LoadKey(); err != nil {
		log.Printf("secrets vault not available (%v) — deploy targets with a secretsTarget will fail until DEVPLATFORM_SECRETS_KEY is set", err)
	} else {
		secretsStore = secretsvault.NewStore(filepath.Join(cfg.DataDir, "secrets"), key)
	}

	pipeline := deploy.NewPipeline(
		&deploy.Builder{},
		deploy.NewVersionStore(filepath.Join(cfg.DataDir, "releases")),
		deploy.NewIISSwapper(deploy.RealCommandRunner{}),
		secretsStore,
	)
	checkoutRoot := filepath.Join(cfg.DataDir, "deploy-checkouts")
	// Approve materializes each checkout with os.MkdirTemp(checkoutRoot,
	// ...), which does not create checkoutRoot itself — without this, every
	// approve on a fresh install fails with ENOENT.
	if err := os.MkdirAll(checkoutRoot, 0o750); err != nil {
		log.Fatalf("failed to create deploy checkout dir %s: %v", checkoutRoot, err)
	}
	deploymentHandlers := &deployment.Handlers{
		Store:        deployment.NewStore(filepath.Join(cfg.DataDir, "deployments")),
		Repos:        store,
		Targets:      targets,
		Pipeline:     pipeline,
		CheckoutRoot: checkoutRoot,
		Audit:        auditLogger,
		Notify:       notifyStore,
		Users:        usersStore,
		Access:       accessStore,
	}

	// BackupDir is the switch: empty means no nightly backup goroutine runs
	// at all, exactly as before this was wired up. Set
	// DEVPLATFORM_BACKUP_DIR to turn it on. backupOne operates on its
	// destination with RemoveAll and Rename, so a destination that
	// coincides with (or contains, or is contained in) the live repo
	// store's root would mean those calls run against real repositories —
	// refuse to start backup rather than risk that.
	if cfg.BackupDir != "" {
		if overlaps, err := backup.PathsOverlap(cfg.BackupDir, store.RootDir()); err != nil {
			log.Printf("backup: could not validate DEVPLATFORM_BACKUP_DIR %q — nightly repository backup is off: %v", cfg.BackupDir, err)
		} else if overlaps {
			log.Printf("backup: DEVPLATFORM_BACKUP_DIR %q overlaps the repository store %q — refusing to enable nightly backup to avoid operating on live repositories", cfg.BackupDir, store.RootDir())
		} else {
			log.Printf("nightly repository backup enabled: copying to %q at %02d:00 daily", cfg.BackupDir, cfg.BackupHour)
			go backup.RunNightly(context.Background(), store, cfg.BackupDir, cfg.BackupHour, 0)
		}
	} else {
		log.Printf("no DEVPLATFORM_BACKUP_DIR configured — nightly repository backup is off")
	}

	router := server.NewRouter(server.Deps{
		GitHandler:     authedGitHandler,
		AuthMiddleware: authMiddleware,
		MergeRequests:  mrHandlers,
		Repos:          repoHandlers,
		Tasks:          taskHandlers,
		Stats:          statsHandlers,
		Audit:          auditHandlers,
		Notifications:  notifyHandlers,
		Deployments:    deploymentHandlers,
		Users:          usersStore,
		Access:         accessStore,
	})

	log.Printf("devplatform listening on %s", cfg.ListenAddr)
	if err := http.ListenAndServe(cfg.ListenAddr, router); err != nil {
		log.Fatal(err)
	}
}
