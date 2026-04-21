// Package server provides a lightweight local HTTP API server that lets callers
// invoke lambda functions via HTTP POST requests while lambit is running.
//
// Route:  POST http://localhost:<port>/<function-name>
// Body:   JSON payload (passed to the lambda as-is)
// Response: 200 OK with the invocation stdout as the body, or 500 on error.
package server

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// InvokeFn is the callback the server calls when an HTTP request arrives.
// functionName is the path segment from the URL; payload is the raw request body.
// It returns the result string and a success flag.
type InvokeFn func(functionName, payload string) (result string, success bool)

// Server is a self-contained HTTP server goroutine.
type Server struct {
	mu      sync.Mutex
	httpSrv *http.Server
	running bool
	port    int
	invoke  InvokeFn
}

// New creates a Server. Call Start() to begin listening.
func New(port int, invoke InvokeFn) *Server {
	return &Server{port: port, invoke: invoke}
}

// Start begins listening on the configured port. It is a no-op if already running.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return nil
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleRequest)

	s.httpSrv = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.port),
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second,
	}

	ln, err := net.Listen("tcp", s.httpSrv.Addr)
	if err != nil {
		return fmt.Errorf("could not bind to :%d: %w", s.port, err)
	}

	s.running = true
	go func() {
		_ = s.httpSrv.Serve(ln)
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()
	return nil
}

// Stop gracefully shuts down the server. It is a no-op if not running.
func (s *Server) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running || s.httpSrv == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.httpSrv.Shutdown(ctx)
	s.running = false
}

// Running returns true if the server is currently listening.
func (s *Server) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// Addr returns the listen address string (e.g. "http://localhost:8080").
func (s *Server) Addr() string {
	return fmt.Sprintf("http://localhost:%d", s.port)
}

func (s *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "only POST is supported", http.StatusMethodNotAllowed)
		return
	}

	// Extract function name from the URL path (strip leading /).
	functionName := strings.TrimPrefix(r.URL.Path, "/")
	if functionName == "" {
		http.Error(w, "path must be /<function-name>", http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "could not read request body", http.StatusBadRequest)
		return
	}
	payload := string(body)
	if payload == "" {
		payload = "{}"
	}

	result, success := s.invoke(functionName, payload)
	if !success {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(result))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(result))
}
