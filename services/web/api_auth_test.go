package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAdministrativeRoutesRequireAuthentication(t *testing.T) {
	srv := New(Config{Admin: AdminConfig{Enabled: true, Username: "admin", Password: "test-password"}}, &mockFileSystem{}, nil)
	paths := []string{
		"/api/v1/feeds", "/api/v1/feeds/status", "/api/v1/feeds/book",
		"/api/v1/feeds/update-all", "/api/v1/feeds/book/update", "/api/v1/feeds/book/episodes/chapter/retry",
		"/api/v1/tokens", "/api/v1/system", "/api/v1/system/errors", "/api/v1/system/recent",
		"/api/v1/system/status", "/api/v1/system/logs", "/api/v1/system/logs/level", "/api/v1/system/logs/download",
		"/api/v1/downloader", "/api/v1/downloader/cookies", "/api/v1/downloader/test", "/api/v1/mcp/config",
		"/api/v1/audiobooks/dirs", "/api/v1/audiobooks/import", "/api/v1/audiobooks/upload", "/api/v1/audiobooks/config",
		"/api/v1/auth/password",
	}
	for _, path := range paths {
		for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
			t.Run(method+path, func(t *testing.T) {
				rec := httptest.NewRecorder()
				srv.Handler.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
				assert.Equal(t, http.StatusUnauthorized, rec.Code)
			})
		}
	}
	for _, path := range []string{"/api/v1/auth/status", "/admin/"} {
		rec := httptest.NewRecorder()
		srv.Handler.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		assert.Equal(t, http.StatusOK, rec.Code)
	}
	for _, cookieAuth := range []bool{false, true} {
		req := httptest.NewRequest("GET", "/api/v1/feeds", nil)
		if cookieAuth {
			token, err := srv.sessions.CreateSession("admin")
			assert.NoError(t, err)
			req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
		} else {
			req.SetBasicAuth("admin", "test-password")
		}
		rec := httptest.NewRecorder()
		srv.Handler.ServeHTTP(rec, req)
		// The handler is reached; this fixture intentionally has no manager.
		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	}
}
