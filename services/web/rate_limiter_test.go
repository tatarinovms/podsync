package web

import (
	"bytes"
	"math"
	"sync"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoginRateLimiter_ByIP(t *testing.T) {
	limiter := NewLoginRateLimiter(3, 1*time.Minute, 10*time.Second)

	// Initially not blocked
	_, blocked := limiter.IsBlocked("1.2.3.4", "user1")
	assert.False(t, blocked)

	// Record failures for user1 from 1.2.3.4
	limiter.RecordFailure("1.2.3.4", "user1")
	limiter.RecordFailure("1.2.3.4", "user1")
	_, blocked = limiter.IsBlocked("1.2.3.4", "user1")
	assert.False(t, blocked)

	// 3rd failure reaches maxByUserIP (3)
	limiter.RecordFailure("1.2.3.4", "user1")
	retryAfter, blocked := limiter.IsBlocked("1.2.3.4", "user1")
	assert.True(t, blocked)
	assert.Greater(t, int(retryAfter.Seconds()), 0)

	// Success clears the failure state
	limiter.RecordSuccess("1.2.3.4", "user1")
	_, blocked = limiter.IsBlocked("1.2.3.4", "user1")
	assert.False(t, blocked)
}

func TestLoginRateLimiter_ByUsername(t *testing.T) {
	limiter := NewLoginRateLimiter(2, 1*time.Minute, 10*time.Second)

	limiter.RecordFailure("1.1.1.1", "targetuser")
	limiter.RecordFailure("2.2.2.2", "targetuser")

	// targetuser should now be blocked globally
	retryAfter, blocked := limiter.IsBlocked("3.3.3.3", "targetuser")
	assert.True(t, blocked)
	assert.Greater(t, int(retryAfter.Seconds()), 0)
}

func TestLoginAPI_RateLimiting429(t *testing.T) {
	srv := New(Config{
		Admin: AdminConfig{
			Enabled:  true,
			Username: "admin",
			Password: "secretpassword",
		},
	}, nil, nil)

	// Fast rate limiter for testing
	srv.rateLimiter = NewLoginRateLimiter(2, 1*time.Minute, 5*time.Second)

	loginReq := func(user, pass string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(LoginRequest{Username: user, Password: pass})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
		req.RemoteAddr = "10.0.0.1:12345"
		w := httptest.NewRecorder()
		srv.handleAuthLogin(w, req)
		return w
	}

	// 1st attempt: wrong password -> 401
	w := loginReq("admin", "wrong1")
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// 2nd attempt: wrong password -> 401
	w = loginReq("admin", "wrong2")
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// 3rd attempt: blocked -> 429 with Retry-After header
	w = loginReq("admin", "wrong3")
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	retryAfter := w.Header().Get("Retry-After")
	require.NotEmpty(t, retryAfter)
	assert.Contains(t, w.Body.String(), "Too many failed login attempts")
}

func TestBasicAuth_RateLimiting429(t *testing.T) {
	srv := New(Config{
		Admin: AdminConfig{
			Enabled:  true,
			Username: "admin",
			Password: "secretpassword",
		},
	}, nil, nil)

	srv.rateLimiter = NewLoginRateLimiter(2, 1*time.Minute, 5*time.Second)

	handler := srv.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	basicReq := func(user, pass string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/feeds", nil)
		req.SetBasicAuth(user, pass)
		req.RemoteAddr = "192.168.1.50:54321"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}

	// 2 failed attempts
	w := basicReq("admin", "bad1")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	w = basicReq("admin", "bad2")
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// 3rd attempt -> 429
	w = basicReq("admin", "bad3")
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.NotEmpty(t, w.Header().Get("Retry-After"))
}

func TestLoginRateLimiter_MaxAttemptsOverflow(t *testing.T) {
	// Pass math.MaxInt as maxAttempts - must clamp to 100 without integer overflow
	limiter := NewLoginRateLimiter(math.MaxInt, 5*time.Minute, 5*time.Minute)
	assert.Equal(t, 100, limiter.MaxAttempts())
	assert.Equal(t, 200, limiter.maxByIP)

	// 1 failed attempt must NOT block
	limiter.RecordFailure("1.2.3.4", "admin")
	_, blocked := limiter.IsBlocked("1.2.3.4", "admin")
	assert.False(t, blocked, "single failure should not trigger lockout when maxAttempts is large")

	// UpdateConfig with math.MaxInt must also clamp
	limiter.UpdateConfig(math.MaxInt, 10*time.Minute, 15*time.Minute)
	assert.Equal(t, 100, limiter.MaxAttempts())
	assert.Equal(t, 200, limiter.maxByIP)
	assert.Equal(t, 10*time.Minute, limiter.Window())
	assert.Equal(t, 15*time.Minute, limiter.Lockout())
}

func TestLoginRateLimiter_ConcurrentAccess(t *testing.T) {
	limiter := NewLoginRateLimiter(5, 5*time.Minute, 5*time.Minute)
	var wg sync.WaitGroup

	// 20 workers calling UpdateConfig concurrently
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(attempt int) {
			defer wg.Done()
			limiter.UpdateConfig((attempt%20)+1, time.Duration((attempt%10)+1)*time.Minute, time.Duration((attempt%10)+1)*time.Minute)
		}(i)
	}

	// 20 workers calling checks and records concurrently
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ip := "192.168.1." + string(rune('0'+(id%10)))
			user := "user" + string(rune('0'+(id%5)))
			limiter.RecordFailure(ip, user)
			limiter.IsBlocked(ip, user)
			limiter.MaxAttempts()
			limiter.Window()
			limiter.Lockout()
			limiter.RecordSuccess(ip, user)
		}(i)
	}

	wg.Wait()
}
