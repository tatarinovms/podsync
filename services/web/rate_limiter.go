package web

import (
	"fmt"
	"math"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type failedAttemptRecord struct {
	count        int
	firstFailed  time.Time
	lastFailed   time.Time
	blockedUntil time.Time
}

// LoginRateLimiter enforces brute-force protection across Web UI login and Basic Auth
type LoginRateLimiter struct {
	mu           sync.Mutex
	window       time.Duration
	lockout      time.Duration
	maxByIP      int
	maxByUserIP  int
	maxByUser    int
	ipAttempts   map[string]*failedAttemptRecord
	pairAttempts map[string]*failedAttemptRecord
	userAttempts map[string]*failedAttemptRecord
}

func NewLoginRateLimiter(maxAttempts int, window time.Duration, lockout time.Duration) *LoginRateLimiter {
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	if window <= 0 {
		window = 5 * time.Minute
	}
	if lockout <= 0 {
		lockout = 5 * time.Minute
	}
	return &LoginRateLimiter{
		window:       window,
		lockout:      lockout,
		maxByIP:      maxAttempts * 2,
		maxByUserIP:  maxAttempts,
		maxByUser:    maxAttempts,
		ipAttempts:   make(map[string]*failedAttemptRecord),
		pairAttempts: make(map[string]*failedAttemptRecord),
		userAttempts: make(map[string]*failedAttemptRecord),
	}
}

// IsBlocked checks if the given IP or username is currently locked out.
func (l *LoginRateLimiter) IsBlocked(ip, username string) (time.Duration, bool) {
	if l == nil {
		return 0, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	cleanUser := strings.ToLower(strings.TrimSpace(username))
	cleanIP := strings.TrimSpace(ip)

	var maxRemaining time.Duration
	blocked := false

	checkRecord := func(rec *failedAttemptRecord) {
		if rec == nil {
			return
		}
		if now.Before(rec.blockedUntil) {
			rem := rec.blockedUntil.Sub(now)
			if rem > maxRemaining {
				maxRemaining = rem
			}
			blocked = true
		}
	}

	if cleanIP != "" {
		checkRecord(l.ipAttempts[cleanIP])
	}
	if cleanUser != "" {
		checkRecord(l.userAttempts[cleanUser])
		if cleanIP != "" {
			checkRecord(l.pairAttempts[cleanIP+":"+cleanUser])
		}
	}

	if blocked {
		sec := time.Duration(math.Ceil(maxRemaining.Seconds())) * time.Second
		if sec < time.Second {
			sec = time.Second
		}
		return sec, true
	}
	return 0, false
}

// RecordFailure records a failed login attempt for the given IP and username.
func (l *LoginRateLimiter) RecordFailure(ip, username string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	cleanUser := strings.ToLower(strings.TrimSpace(username))
	cleanIP := strings.TrimSpace(ip)

	record := func(m map[string]*failedAttemptRecord, key string, threshold int) {
		if key == "" {
			return
		}
		rec, exists := m[key]
		if !exists || now.Sub(rec.firstFailed) > l.window {
			rec = &failedAttemptRecord{
				count:       1,
				firstFailed: now,
				lastFailed:  now,
			}
			m[key] = rec
		} else {
			rec.count++
			rec.lastFailed = now
		}

		if rec.count >= threshold {
			rec.blockedUntil = now.Add(l.lockout)
		}
	}

	if cleanIP != "" {
		record(l.ipAttempts, cleanIP, l.maxByIP)
	}
	if cleanUser != "" {
		record(l.userAttempts, cleanUser, l.maxByUser)
		if cleanIP != "" {
			record(l.pairAttempts, cleanIP+":"+cleanUser, l.maxByUserIP)
		}
	}
}

// RecordSuccess clears failure counters upon successful login.
func (l *LoginRateLimiter) RecordSuccess(ip, username string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	cleanUser := strings.ToLower(strings.TrimSpace(username))
	cleanIP := strings.TrimSpace(ip)

	if cleanIP != "" {
		delete(l.ipAttempts, cleanIP)
	}
	if cleanUser != "" {
		delete(l.userAttempts, cleanUser)
		if cleanIP != "" {
			delete(l.pairAttempts, cleanIP+":"+cleanUser)
		}
	}
}

// getClientIP extracts remote IP from Request.
func getClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		ip := strings.TrimSpace(parts[0])
		if ip != "" {
			return ip
		}
	}
	if xrip := strings.TrimSpace(r.Header.Get("X-Real-IP")); xrip != "" {
		return xrip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return r.RemoteAddr
}

func (l *LoginRateLimiter) MaxAttempts() int {
	if l == nil {
		return 5
	}
	return l.maxByUser
}

func (l *LoginRateLimiter) Window() time.Duration {
	if l == nil {
		return 5 * time.Minute
	}
	return l.window
}

func (l *LoginRateLimiter) Lockout() time.Duration {
	if l == nil {
		return 5 * time.Minute
	}
	return l.lockout
}

func formatConfigDuration(d time.Duration) string {
	if d <= 0 {
		return "5m"
	}
	if d%(24*time.Hour) == 0 {
		return fmt.Sprintf("%dh", d/time.Hour)
	}
	if d%time.Hour == 0 {
		return fmt.Sprintf("%dh", d/time.Hour)
	}
	if d%time.Minute == 0 {
		return fmt.Sprintf("%dm", d/time.Minute)
	}
	if d%time.Second == 0 {
		return fmt.Sprintf("%ds", d/time.Second)
	}
	return d.String()
}
