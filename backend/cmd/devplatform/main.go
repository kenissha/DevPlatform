package main

import (
	"log"
	"net/http"

	"github.com/kenissha/DevPlatform/backend/internal/config"
	"github.com/kenissha/DevPlatform/backend/internal/server"
)

func main() {
	cfg := config.Load()
	router := server.NewRouter()

	log.Printf("devplatform listening on %s (data dir: %s)", cfg.ListenAddr, cfg.DataDir)
	if err := http.ListenAndServe(cfg.ListenAddr, router); err != nil {
		log.Fatal(err)
	}
}
