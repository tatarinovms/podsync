package mcp

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"sync"
)

type sseClient struct {
	id       string
	msgChan  chan []byte
	doneChan chan struct{}
}

// HTTPHandler returns an http.Handler that serves both direct JSON-RPC and SSE transports
type HTTPHandler struct {
	server  *Server
	clients map[string]*sseClient
	mu      sync.RWMutex
}

// NewHTTPHandler creates a new HTTPHandler for the MCP server
func NewHTTPHandler(server *Server) *HTTPHandler {
	return &HTTPHandler{
		server:  server,
		clients: make(map[string]*sseClient),
	}
}

func (h *HTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	switch {
	case stringsHasSuffix(path, "/sse") && r.Method == http.MethodGet:
		h.handleSSE(w, r)
	case stringsHasSuffix(path, "/messages") && r.Method == http.MethodPost:
		h.handleMessages(w, r)
	case r.Method == http.MethodPost:
		h.handleDirectJSONRPC(w, r)
	default:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name":"podsync-mcp","protocol":"2024-11-05","status":"ready"}`))
	}
}

func (h *HTTPHandler) handleDirectJSONRPC(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read body: %v", err), http.StatusBadRequest)
		return
	}

	respBytes, err := h.server.HandleMessage(r.Context(), body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Internal error: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(respBytes)
}

func (h *HTTPHandler) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Generate session ID
	sessionBytes := make([]byte, 16)
	_, _ = rand.Read(sessionBytes)
	sessionID := hex.EncodeToString(sessionBytes)

	client := &sseClient{
		id:       sessionID,
		msgChan:  make(chan []byte, 32),
		doneChan: make(chan struct{}),
	}

	h.mu.Lock()
	h.clients[sessionID] = client
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		delete(h.clients, sessionID)
		h.mu.Unlock()
		close(client.doneChan)
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Base path of the message endpoint
	msgPath := r.URL.Path
	if stringsHasSuffix(msgPath, "/sse") {
		msgPath = msgPath[:len(msgPath)-4] + "/messages"
	}
	endpointURL := fmt.Sprintf("%s?sessionId=%s", msgPath, sessionID)

	// Send endpoint event
	fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", endpointURL)
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-client.msgChan:
			if !ok {
				return
			}
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", string(msg))
			flusher.Flush()
		}
	}
}

func (h *HTTPHandler) handleMessages(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionId")
	if sessionID == "" {
		http.Error(w, "Missing sessionId query parameter", http.StatusBadRequest)
		return
	}

	h.mu.RLock()
	client, exists := h.clients[sessionID]
	h.mu.RUnlock()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read body: %v", err), http.StatusBadRequest)
		return
	}

	respBytes, err := h.server.HandleMessage(r.Context(), body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Internal error: %v", err), http.StatusInternalServerError)
		return
	}

	// If SSE client exists, send response over SSE stream as well
	if exists && client != nil && respBytes != nil {
		select {
		case client.msgChan <- respBytes:
		default:
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	if respBytes != nil {
		_, _ = w.Write(respBytes)
	}
}

func stringsHasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}
