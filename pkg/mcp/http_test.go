package mcp

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHTTPHandler_RequestBodyLimit(t *testing.T) {
	server := NewServer(nil)
	handler := NewHTTPHandler(server)

	// 1. Normal payload fits under 4 MB
	normalBody := []byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(normalBody))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)

	// 2. Oversized payload (> 4 MB) must be rejected
	oversized := make([]byte, maxMCPRequestBody+1024)
	reqLarge := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(oversized))
	rrLarge := httptest.NewRecorder()
	handler.ServeHTTP(rrLarge, reqLarge)
	assert.Equal(t, http.StatusBadRequest, rrLarge.Code)
	assert.Contains(t, rrLarge.Body.String(), "Failed to read body")
}
