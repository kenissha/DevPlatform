package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/kenissha/DevPlatform/backend/internal/auth"
	"github.com/kenissha/DevPlatform/backend/internal/config"
	"github.com/kenissha/DevPlatform/backend/internal/gitauth"
	"github.com/kenissha/DevPlatform/backend/internal/gitserver"
	"github.com/kenissha/DevPlatform/backend/internal/gitstats"
	"github.com/kenissha/DevPlatform/backend/internal/mergerequest"
	"github.com/kenissha/DevPlatform/backend/internal/repoapi"
	"github.com/kenissha/DevPlatform/backend/internal/repostore"
	"github.com/kenissha/DevPlatform/backend/internal/server"
	"github.com/kenissha/DevPlatform/backend/internal/taskboard"
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
	mrHandlers := &mergerequest.Handlers{
		Store: mergerequest.NewStore(filepath.Join(cfg.DataDir, "merge-requests")),
		Repos: store,
	}
	repoHandlers := &repoapi.Handlers{Repos: store}
	taskHandlers := &taskboard.Handlers{
		Store: taskboard.NewStore(filepath.Join(cfg.DataDir, "tasks")),
		Repos: store,
	}
	statsHandlers := &gitstats.Handlers{Repos: store}
	router := server.NewRouter(
		authedGitHandler, authMiddleware, mrHandlers, repoHandlers, taskHandlers, statsHandlers,
	)

	log.Printf("devplatform listening on %s", cfg.ListenAddr)
	if err := http.ListenAndServe(cfg.ListenAddr, router); err != nil {
		log.Fatal(err)
	}
}
