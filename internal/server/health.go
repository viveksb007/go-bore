package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/viveksb007/go-bore/internal/logging"
)

// HealthServer provides HTTP health check endpoints
type HealthServer struct {
	port       int
	httpServer *http.Server
}

// NewHealthServer creates a new health check server on the specified port
func NewHealthServer(port int) *HealthServer {
	return &HealthServer{
		port: port,
	}
}

// Start starts the health check HTTP server
func (h *HealthServer) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", h.healthHandler)
	mux.HandleFunc("/healthz", h.healthHandler)
	mux.HandleFunc("/ready", h.healthHandler)
	mux.HandleFunc("/", h.healthHandler)

	h.httpServer = &http.Server{
		Addr:         fmt.Sprintf(":%d", h.port),
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	logging.Info("health check server starting", "port", h.port)

	go func() {
		if err := h.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logging.Error("health check server error", "error", err)
		}
	}()

	return nil
}

// Stop gracefully shuts down the health check server
func (h *HealthServer) Stop() error {
	if h.httpServer == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	logging.Debug("shutting down health check server")
	return h.httpServer.Shutdown(ctx)
}

func (h *HealthServer) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}
