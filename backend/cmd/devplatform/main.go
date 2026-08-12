package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/kenissha/DevPlatform/backend/internal/audit"
	"github.com/kenissha/DevPlatform/backend/internal/auth"
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
	mrHandlers := &mergerequest.Handlers{
		Store:  mergerequest.NewStore(filepath.Join(cfg.DataDir, "merge-requests")),
		Repos:  store,
		Audit:  auditLogger,
		Notify: notifyStore,
		Users:  usersStore,
	}
	repoHandlers := &repoapi.Handlers{Repos: store, Audit: auditLogger}
	taskHandlers := &taskboard.Handlers{
		Store:  taskboard.NewStore(filepath.Join(cfg.DataDir, "tasks")),
		Repos:  store,
		Audit:  auditLogger,
		Notify: notifyStore,
	}
	statsHandlers := &gitstats.Handlers{Repos: store}
	auditHandlers := &audit.Handlers{Logger: auditLogger}
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
	deploymentHandlers := &deployment.Handlers{
		Store:        deployment.NewStore(filepath.Join(cfg.DataDir, "deployments")),
		Repos:        store,
		Targets:      targets,
		Pipeline:     pipeline,
		CheckoutRoot: checkoutRoot,
		Audit:        auditLogger,
		Notify:       notifyStore,
		Users:        usersStore,
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
	})

	log.Printf("devplatform listening on %s", cfg.ListenAddr)
	if err := http.ListenAndServe(cfg.ListenAddr, router); err != nil {
		log.Fatal(err)
	}
}
