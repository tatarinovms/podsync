package web

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPIBodyLimitPreservesUploads(t *testing.T) {
	for _, path := range []string{"/api/v1/auth/login", "/api/v1/feeds/show", "/api/v1/feeds/show/cover", "/api/v1/personal-feeds/personal/upload"} {
		t.Run(path, func(t *testing.T) {
			var readErr error
			h := limitAPIRequestBody(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				_, readErr = io.Copy(io.Discard, r.Body)
			}))
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(strings.Repeat("x", 2<<20)))
			// A misleading Content-Type must not bypass limits on JSON routes.
			req.Header.Set("Content-Type", "multipart/form-data")
			h.ServeHTTP(httptest.NewRecorder(), req)
			if strings.HasSuffix(path, "/cover") || strings.HasSuffix(path, "/upload") {
				require.NoError(t, readErr)
			} else {
				var tooLarge *http.MaxBytesError
				require.ErrorAs(t, readErr, &tooLarge)
			}
		})
	}
}

func TestLoginBodyIsBounded(t *testing.T) {
	srv := New(Config{Admin: AdminConfig{Enabled: true}}, nil, nil)
	t.Cleanup(func() { require.NoError(t, srv.Close()) })
	body, err := json.Marshal(LoginRequest{Username: "admin", Password: strings.Repeat("x", 2<<20)})
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(string(body))))
	require.Equal(t, http.StatusBadRequest, rec.Code)
}
