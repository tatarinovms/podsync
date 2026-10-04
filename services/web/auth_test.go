package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionManager(t *testing.T) {
	sm := NewSessionManager()

	// 1. Create session
	token, err := sm.CreateSession("testuser")
	require.NoError(t, err)
	assert.NotEmpty(t, token)

	// 2. Validate session
	user, valid := sm.ValidateToken(token)
	assert.True(t, valid)
	assert.Equal(t, "testuser", user)

	// 3. Invalid token
	_, valid = sm.ValidateToken("non-existent-token")
	assert.False(t, valid)

	// 4. Revoke token
	sm.RevokeToken(token)
	_, valid = sm.ValidateToken(token)
	assert.False(t, valid)
}

func TestAuthMiddleware(t *testing.T) {
	cfg := Config{
		Admin: AdminConfig{
			Enabled:  true,
			Username: "admin",
			Password: "secretpassword",
		},
	}

	srv := New(cfg, http.Dir("."), nil)

	// 1. Unauthenticated API request should get 401
	req := httptest.NewRequest(http.MethodGet, "/api/v1/feeds", nil)
	rr := httptest.NewRecorder()
	srv.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rr, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)

	// 2. Request with Basic Auth
	req = httptest.NewRequest(http.MethodGet, "/api/v1/feeds", nil)
	req.SetBasicAuth("admin", "secretpassword")
	rr = httptest.NewRecorder()
	srv.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)

	// 3. Request with Session Cookie
	token, err := srv.sessions.CreateSession("admin")
	require.NoError(t, err)

	req = httptest.NewRequest(http.MethodGet, "/api/v1/feeds", nil)
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: token,
	})
	rr = httptest.NewRecorder()
	srv.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)

	// 4. Login flow
	loginBody, _ := json.Marshal(LoginRequest{
		Username: "admin",
		Password: "secretpassword",
	})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(loginBody))
	rr = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)

	cookies := rr.Result().Cookies()
	require.NotEmpty(t, cookies)
	assert.Equal(t, sessionCookieName, cookies[0].Name)
}

func TestAdminUIDelivery(t *testing.T) {
	cfg := Config{
		Admin: AdminConfig{
			Enabled:  true,
			Username: "admin",
			Password: "secretpassword",
		},
	}

	srv := New(cfg, http.Dir("."), nil)

	// GET /admin should redirect to /admin/
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rr := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusMovedPermanently, rr.Code)

	// GET /admin/ serves index.html
	req = httptest.NewRequest(http.MethodGet, "/admin/", nil)
	rr = httptest.NewRecorder()
	srv.Handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "Podsync Management Console")
}
