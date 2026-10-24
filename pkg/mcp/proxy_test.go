package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsReachableUsesReadiness(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health/ready" {
			w.WriteHeader(http.StatusOK)
			return
		}
		// Download failures can make the legacy diagnostic endpoint return 503
		// while the running daemon remains available to MCP clients.
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	assert.True(t, NewHTTPClientManager(server.URL, "", "").IsReachable(context.Background()))
}
