package web

import (
	"net/http"
	"net/http/httptest"
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
