package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync/atomic"
	"time"
)

type healthServer struct {
	port      string
	startedAt time.Time
	self      string // logged-in account name

	// pointer to the active forwarder for live stats (may be nil before ready)
	active *atomic.Pointer[forwarder]
}

type healthResponse struct {
	Status          string `json:"status"`
	Uptime          string `json:"uptime"`
	LoggedInAs      string `json:"logged_in_as"`
	Source          string `json:"source,omitempty"`
	Target          string `json:"target,omitempty"`
	TotalForwarded  int64  `json:"total_forwarded"`
	LastForwardedAt string `json:"last_forwarded_at,omitempty"`
}

func newHealthServer(port string, active *atomic.Pointer[forwarder]) *healthServer {
	return &healthServer{
		port:      port,
		startedAt: time.Now(),
		active:    active,
	}
}

func (h *healthServer) SetSelf(name string) {
	h.self = name
}

func (h *healthServer) Start() {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", h.handleHealth)
	mux.HandleFunc("/ping", h.handlePing)
	mux.HandleFunc("/", h.handleHealth)

	addr := ":" + h.port
	log.Printf("health server listening on %s", addr)
	go func() {
		if err := http.ListenAndServe(addr, mux); err != nil && err != http.ErrServerClosed {
			log.Printf("health server error: %v", err)
		}
	}()
}

// handlePing is a minimal liveness probe — always returns 200 OK.
func (h *healthServer) handlePing(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("pong"))
}

// handleHealth returns a JSON status document.
func (h *healthServer) handleHealth(w http.ResponseWriter, _ *http.Request) {
	resp := healthResponse{
		Status:     "ok",
		Uptime:     formatDuration(time.Since(h.startedAt)),
		LoggedInAs: h.self,
	}

	if f := h.active.Load(); f != nil {
		resp.Source = f.sourceName
		resp.Target = f.targetName
		resp.TotalForwarded = f.totalForwarded.Load()

		if ts := f.lastForwardedAt.Load(); ts != 0 {
			resp.LastForwardedAt = time.Unix(ts, 0).UTC().Format(time.RFC3339)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh%dm%ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm%ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}
