package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/alexnthnz/distri-kv/internal/node"
	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"
)

// Server represents the HTTP API server
type Server struct {
	node      *node.Node
	router    *mux.Router
	server    *http.Server
	logger    *logrus.Logger
	port      int
	isRunning bool
}

// NewServer creates a new HTTP API server
func NewServer(node *node.Node, port int, logger *logrus.Logger) *Server {
	server := &Server{
		node:   node,
		router: mux.NewRouter(),
		logger: logger,
		port:   port,
	}

	server.setupRoutes()
	return server
}

// setupRoutes sets up the HTTP routes
func (s *Server) setupRoutes() {
	// API routes
	api := s.router.PathPrefix("/api/v1").Subrouter()

	// Key-value operations
	api.HandleFunc("/kv/{key}", s.handleGet).Methods("GET")
	api.HandleFunc("/kv/{key}", s.handlePut).Methods("PUT")
	api.HandleFunc("/kv/{key}", s.handleDelete).Methods("DELETE")

	// Cluster operations
	api.HandleFunc("/cluster/stats", s.handleStats).Methods("GET")
	api.HandleFunc("/cluster/health", s.handleHealth).Methods("GET")

	// Admin operations
	api.HandleFunc("/admin/stats", s.handleAdminStats).Methods("GET")

	// Middleware
	s.router.Use(s.loggingMiddleware)
	s.router.Use(s.corsMiddleware)
}

// Start starts the HTTP server
func (s *Server) Start() error {
	if s.isRunning {
		return fmt.Errorf("server is already running")
	}

	s.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.port),
		Handler:      s.router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	s.isRunning = true
	s.logger.Infof("Starting HTTP API server on port %d", s.port)

	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Errorf("HTTP server error: %v", err)
		}
	}()

	return nil
}

// Stop stops the HTTP server
func (s *Server) Stop() error {
	if !s.isRunning {
		return nil
	}

	s.logger.Info("Stopping HTTP API server")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := s.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown server: %w", err)
	}

	s.isRunning = false
	s.logger.Info("HTTP API server stopped")
	return nil
}

// handleGet handles GET requests for key-value retrieval
func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	key := vars["key"]

	if key == "" {
		s.writeErrorResponse(w, http.StatusBadRequest, "Key is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	value, err := s.node.Get(ctx, key)
	if err != nil {
		s.logger.Errorf("Failed to get key %s: %v", key, err)
		s.writeErrorResponse(w, http.StatusInternalServerError, fmt.Sprintf("Failed to get key: %v", err))
		return
	}

	if value == nil {
		s.writeErrorResponse(w, http.StatusNotFound, "Key not found")
		return
	}

	response := map[string]interface{}{
		"key":   key,
		"value": string(value),
	}

	s.writeJSONResponse(w, http.StatusOK, response)
}

// handlePut handles PUT requests for key-value storage
func (s *Server) handlePut(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	key := vars["key"]

	if key == "" {
		s.writeErrorResponse(w, http.StatusBadRequest, "Key is required")
		return
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.writeErrorResponse(w, http.StatusBadRequest, "Failed to read request body")
		return
	}
	defer r.Body.Close()

	// Parse JSON request
	var req struct {
		Value string `json:"value"`
	}

	if err := json.Unmarshal(body, &req); err != nil {
		// If not JSON, treat body as raw value
		req.Value = string(body)
	}

	if req.Value == "" {
		s.writeErrorResponse(w, http.StatusBadRequest, "Value is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if err := s.node.Put(ctx, key, []byte(req.Value)); err != nil {
		s.logger.Errorf("Failed to put key %s: %v", key, err)
		s.writeErrorResponse(w, http.StatusInternalServerError, fmt.Sprintf("Failed to store key: %v", err))
		return
	}

	response := map[string]interface{}{
		"key":    key,
		"value":  req.Value,
		"status": "stored",
	}

	s.writeJSONResponse(w, http.StatusOK, response)
}

// handleDelete handles DELETE requests for key-value deletion
func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	key := vars["key"]

	if key == "" {
		s.writeErrorResponse(w, http.StatusBadRequest, "Key is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if err := s.node.Delete(ctx, key); err != nil {
		s.logger.Errorf("Failed to delete key %s: %v", key, err)
		s.writeErrorResponse(w, http.StatusInternalServerError, fmt.Sprintf("Failed to delete key: %v", err))
		return
	}

	response := map[string]interface{}{
		"key":    key,
		"status": "deleted",
	}

	s.writeJSONResponse(w, http.StatusOK, response)
}

// handleStats handles cluster statistics requests
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	stats := s.node.GetStats()
	s.writeJSONResponse(w, http.StatusOK, stats)
}

// handleHealth handles health check requests
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	isHealthy := s.node.IsHealthy(ctx)

	response := map[string]interface{}{
		"healthy":   isHealthy,
		"node_id":   s.node.GetNodeID(),
		"address":   s.node.GetAddress(),
		"timestamp": time.Now().Unix(),
	}

	status := http.StatusOK
	if !isHealthy {
		status = http.StatusServiceUnavailable
	}

	s.writeJSONResponse(w, status, response)
}

// handleAdminStats handles detailed administrative statistics
func (s *Server) handleAdminStats(w http.ResponseWriter, r *http.Request) {
	stats := s.node.GetStats()

	// Add additional admin-specific information
	adminStats := map[string]interface{}{
		"node_stats": stats,
		"server_info": map[string]interface{}{
			"port":       s.port,
			"is_running": s.isRunning,
			"uptime":     time.Now().Unix(), // In a real implementation, track actual uptime
		},
	}

	s.writeJSONResponse(w, http.StatusOK, adminStats)
}

// writeJSONResponse writes a JSON response
func (s *Server) writeJSONResponse(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		s.logger.Errorf("Failed to encode JSON response: %v", err)
	}
}

// writeErrorResponse writes an error response
func (s *Server) writeErrorResponse(w http.ResponseWriter, status int, message string) {
	response := map[string]interface{}{
		"error":     message,
		"status":    status,
		"timestamp": time.Now().Unix(),
	}

	s.writeJSONResponse(w, status, response)
}

// loggingMiddleware logs HTTP requests
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Create a response writer wrapper to capture status code
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(rw, r)

		duration := time.Since(start)

		s.logger.Infof("%s %s %d %v %s",
			r.Method,
			r.RequestURI,
			rw.statusCode,
			duration,
			r.RemoteAddr,
		)
	})
}

// corsMiddleware adds CORS headers
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// responseWriter is a wrapper for http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// APIResponse represents a standard API response
type APIResponse struct {
	Success   bool        `json:"success"`
	Data      interface{} `json:"data,omitempty"`
	Error     string      `json:"error,omitempty"`
	Timestamp int64       `json:"timestamp"`
}

// BatchRequest represents a batch operation request
type BatchRequest struct {
	Operations []BatchOperation `json:"operations"`
}

// BatchOperation represents a single operation in a batch
type BatchOperation struct {
	Type  string `json:"type"` // "get", "put", "delete"
	Key   string `json:"key"`
	Value string `json:"value,omitempty"`
}

// BatchResponse represents a batch operation response
type BatchResponse struct {
	Results []BatchResult `json:"results"`
}

// BatchResult represents the result of a single batch operation
type BatchResult struct {
	Key     string `json:"key"`
	Success bool   `json:"success"`
	Value   string `json:"value,omitempty"`
	Error   string `json:"error,omitempty"`
}

// handleBatch handles batch operations (future enhancement)
func (s *Server) handleBatch(w http.ResponseWriter, r *http.Request) {
	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.writeErrorResponse(w, http.StatusBadRequest, "Failed to read request body")
		return
	}
	defer r.Body.Close()

	var batchReq BatchRequest
	if err := json.Unmarshal(body, &batchReq); err != nil {
		s.writeErrorResponse(w, http.StatusBadRequest, "Invalid JSON format")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	results := make([]BatchResult, len(batchReq.Operations))

	for i, op := range batchReq.Operations {
		result := BatchResult{Key: op.Key}

		switch op.Type {
		case "get":
			value, err := s.node.Get(ctx, op.Key)
			if err != nil {
				result.Error = err.Error()
			} else if value == nil {
				result.Error = "key not found"
			} else {
				result.Success = true
				result.Value = string(value)
			}

		case "put":
			err := s.node.Put(ctx, op.Key, []byte(op.Value))
			if err != nil {
				result.Error = err.Error()
			} else {
				result.Success = true
			}

		case "delete":
			err := s.node.Delete(ctx, op.Key)
			if err != nil {
				result.Error = err.Error()
			} else {
				result.Success = true
			}

		default:
			result.Error = "invalid operation type"
		}

		results[i] = result
	}

	response := BatchResponse{Results: results}
	s.writeJSONResponse(w, http.StatusOK, response)
}

// GetPort returns the server port
func (s *Server) GetPort() int {
	return s.port
}

// IsRunning returns true if the server is running
func (s *Server) IsRunning() bool {
	return s.isRunning
}
