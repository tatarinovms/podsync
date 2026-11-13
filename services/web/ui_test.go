package web

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdminUIEmbeddedAssets(t *testing.T) {
	srv := &Server{}
	handler := srv.adminUIHandler()

	tests := []struct {
		path            string
		expectedStatus  int
		expectedType    string
		containsContent string
	}{
		{
			path:            "/admin",
			expectedStatus:  http.StatusOK,
			expectedType:    "text/html; charset=utf-8",
			containsContent: "Podsync Management Console",
		},
		{
			path:            "/admin/index.html",
			expectedStatus:  http.StatusOK,
			expectedType:    "text/html; charset=utf-8",
			containsContent: "bootstrap.bundle.min.js",
		},
		{
			path:            "/admin/bootstrap.min.css",
			expectedStatus:  http.StatusOK,
			expectedType:    "text/css",
			containsContent: "--bs-blue",
		},
		{
			path:            "/admin/bootstrap.bundle.min.js",
			expectedStatus:  http.StatusOK,
			expectedType:    "application/javascript",
			containsContent: "Bootstrap",
		},
		{
			path:            "/admin/bootstrap-icons.min.css",
			expectedStatus:  http.StatusOK,
			expectedType:    "text/css",
			containsContent: "bootstrap-icons",
		},
		{
			path:           "/admin/fonts/bootstrap-icons.woff2",
			expectedStatus: http.StatusOK,
			expectedType:   "font/woff2",
		},
		{
			path:           "/admin/fonts/bootstrap-icons.woff",
			expectedStatus: http.StatusOK,
			expectedType:   "font/woff",
		},
		{
			path:            "/admin/admin.js",
			expectedStatus:  http.StatusOK,
			expectedType:    "application/javascript",
			containsContent: "toastEl",
		},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			require.Equal(t, tc.expectedStatus, rec.Code)
			contentType := rec.Header().Get("Content-Type")
			assert.True(t, strings.HasPrefix(contentType, tc.expectedType),
				"path %s: expected Content-Type prefix %s, got %s", tc.path, tc.expectedType, contentType)

			if tc.containsContent != "" {
				assert.Contains(t, rec.Body.String(), tc.containsContent)
			}
		})
	}
}

func TestAdminUIAssetCaching(t *testing.T) {
	srv := &Server{}
	handler := srv.adminUIHandler()

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/admin/admin.js", nil))
	require.Equal(t, http.StatusOK, first.Code)
	etag := first.Header().Get("ETag")
	require.NotEmpty(t, etag, "embedded assets must expose a strong validator")
	require.Equal(t, "no-cache, must-revalidate", first.Header().Get("Cache-Control"))

	second := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/admin.js", nil)
	req.Header.Set("If-None-Match", etag)
	handler.ServeHTTP(second, req)
	require.Equal(t, http.StatusNotModified, second.Code)
	require.Empty(t, second.Body.String())
}

func TestAdminUIAssetCompression(t *testing.T) {
	srv := &Server{}
	handler := srv.adminUIHandler()

	plain := httptest.NewRecorder()
	handler.ServeHTTP(plain, httptest.NewRequest(http.MethodGet, "/admin/admin.css", nil))
	require.Equal(t, http.StatusOK, plain.Code)
	require.Empty(t, plain.Header().Get("Content-Encoding"))

	compressed := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/admin.css", nil)
	req.Header.Set("Accept-Encoding", "gzip, deflate")
	handler.ServeHTTP(compressed, req)
	require.Equal(t, http.StatusOK, compressed.Code)
	require.Equal(t, "gzip", compressed.Header().Get("Content-Encoding"))
	require.Equal(t, "Accept-Encoding", compressed.Header().Get("Vary"))

	reader, err := gzip.NewReader(compressed.Body)
	require.NoError(t, err)
	decoded, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	require.Equal(t, plain.Body.Bytes(), decoded)
	require.Less(t, compressed.Body.Len(), plain.Body.Len(), "gzip must reduce the transfer size")
}

func TestAdminUIAssetRejectsGzipWhenDisabled(t *testing.T) {
	srv := &Server{}
	handler := srv.adminUIHandler()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/admin.js", nil)
	req.Header.Set("Accept-Encoding", "gzip;q=0, identity")
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Empty(t, rec.Header().Get("Content-Encoding"))
}

func TestAdminUIAssetMirror(t *testing.T) {
	// AGENTS.md requires the embedded assets and the root html/ mirror to stay
	// byte-for-byte identical; keep that invariant enforced by the test suite.
	for _, name := range []string{"index.html", "admin.css", "admin.js", "theme.js", "select.js"} {
		embedded, err := adminContent.ReadFile("ui/" + name)
		require.NoError(t, err)
		mirrored, err := os.ReadFile(filepath.Join("..", "..", "html", name))
		require.NoError(t, err, "html/%s must exist", name)
		require.Equal(t, string(embedded), string(mirrored), "html/%s is out of sync with services/web/ui/%s", name, name)
	}
}

func TestAdminUIRejectsMissingAssets(t *testing.T) {
	srv := &Server{}
	handler := srv.adminUIHandler()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/missing-file.js", nil))
	require.Equal(t, http.StatusNotFound, rec.Code)

	spa := httptest.NewRecorder()
	handler.ServeHTTP(spa, httptest.NewRequest(http.MethodGet, "/admin/feed/some-id", nil))
	require.Equal(t, http.StatusOK, spa.Code)
	require.Contains(t, spa.Body.String(), "Podsync Management Console")
}

func TestAcceptsGzip(t *testing.T) {
	for header, want := range map[string]bool{
		"":                    false,
		"gzip":                true,
		"GZIP, deflate":       true,
		"deflate":             false,
		"gzip;q=0":            false,
		"gzip;q=0.5":          true,
		"br, *;q=0.1":         true,
		"*;q=0, gzip;q=0":     false,
		"identity;q=0, gzip":  true,
		"deflate, br, gzip":   true,
		"gzip; q=0.0, br;q=1": false,
	} {
		require.Equal(t, want, acceptsGzip(header), "header %q", header)
	}
}
