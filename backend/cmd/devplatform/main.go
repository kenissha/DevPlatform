package main

import (
	"log"
	"net/http"

	"github.com/kenissha/DevPlatform/backend/internal/server"
)

func main() {
	router := server.NewRouter()

	addr := ":8080"
	log.Printf("devplatform listening on %s", addr)
	if err := http.ListenAndServe(addr, router); err != nil {
		log.Fatal(err)
	}
}
