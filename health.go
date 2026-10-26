package main

import (
	"log"
	"net/http"
)

type pingServer struct {
	port string
}

func newPingServer(port string) *pingServer {
	return &pingServer{
		port: port,
	}
}

func (s *pingServer) Start() {
	mux := http.NewServeMux()
	mux.HandleFunc("/ping", s.handlePing)
	mux.HandleFunc("/", s.handlePing)

	addr := ":" + s.port
	log.Printf("ping server listening on %s", addr)
	go func() {
		if err := http.ListenAndServe(addr, mux); err != nil && err != http.ErrServerClosed {
			log.Printf("ping server error: %v", err)
		}
	}()
}

// handlePing is a minimal liveness probe — always returns 200 OK with plain "pong".
func (s *pingServer) handlePing(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("pong\n"))
}
