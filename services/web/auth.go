package web

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

const (
	sessionCookieName = "podsync_session"
	sessionTTL        = 7 * 24 * time.Hour
)

type Session struct {
	Username  string
	ExpiresAt time.Time
}

type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]Session
}

func NewSessionManager() *SessionManager {
	sm := &SessionManager{
		sessions: make(map[string]Session),
	}

	// Periodically clean up expired sessions
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		for range ticker.C {
			sm.cleanup()
		}
	}()

	return sm
}

func (sm *SessionManager) CreateSession(username string) (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	token := hex.EncodeToString(bytes)

	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.sessions[token] = Session{
		Username:  username,
		ExpiresAt: time.Now().Add(sessionTTL),
	}

	return token, nil
}

func (sm *SessionManager) ValidateToken(token string) (string, bool) {
	if token == "" {
		return "", false
	}

	sm.mu.RLock()
	defer sm.mu.RUnlock()

	session, exists := sm.sessions[token]
	if !exists {
		return "", false
	}

	if time.Now().After(session.ExpiresAt) {
		return "", false
	}

	return session.Username, true
}

func (sm *SessionManager) RevokeToken(token string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.sessions, token)
}

func (sm *SessionManager) cleanup() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	now := time.Now()
	for token, session := range sm.sessions {
		if now.After(session.ExpiresAt) {
			delete(sm.sessions, token)
		}
	}
}

// AuthMiddleware protects routes when admin authentication is enabled
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.cfg.Admin.Enabled {
			http.NotFound(w, r)
			return
		}

		// Allow public access to login API and assets
		path := r.URL.Path
		if path == "/api/v1/auth/login" || path == "/api/v1/auth/status" {
			next.ServeHTTP(w, r)
			return
		}

		// Check 1: Session Cookie
		if cookie, err := r.Cookie(sessionCookieName); err == nil {
			if _, ok := s.sessions.ValidateToken(cookie.Value); ok {
				next.ServeHTTP(w, r)
				return
			}
		}

		// Check 2: HTTP Basic Auth (useful for CLI/curl scripts)
		username, password, hasBasic := r.BasicAuth()
		if hasBasic {
			if s.checkCredentials(username, password) {
				next.ServeHTTP(w, r)
				return
			}
		}

		// If unauthenticated API call -> return 401 JSON
		if strings.HasPrefix(path, "/api/") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"Unauthorized"}`))
			return
		}

		// If unauthenticated web request -> serve admin UI (which will show login form)
		// Or redirect to /admin
		if strings.HasPrefix(path, "/admin") {
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set("WWW-Authenticate", `Basic realm="Podsync Admin"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	})
}

func (s *Server) checkCredentials(username, password string) bool {
	expectedUser := s.cfg.Admin.Username
	expectedPass := s.cfg.Admin.Password

	if expectedUser == "" || expectedPass == "" {
		log.Warn("Admin enabled but username or password is empty")
		return false
	}

	return username == expectedUser && password == expectedPass
}
