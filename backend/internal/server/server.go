package server

import (
	"encoding/json"
	"log"
	"net/http"
)

// NewRouter builds the top-level HTTP router. Later packages extend this
// with additional routes (git smart-HTTP, task API, ...).
func NewRouter() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealth)
	return mux
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
		log.Printf("handleHealth: failed to encode response: %v", err)
	}
}
