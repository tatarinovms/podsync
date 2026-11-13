// REST API routing, shared request/response types and authentication handlers.
package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/mxpv/podsync/pkg/feed"
	log "github.com/sirupsen/logrus"
)

var validFeedIDRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type AddFeedRequest struct {
	ID               string       `json:"id"`
	URL              string       `json:"url"`
	Format           string       `json:"format"`
	Quality          string       `json:"quality"`
	MaxHeight        int          `json:"max_height"`
	PageSize         int          `json:"page_size"`
	CronSchedule     string       `json:"cron_schedule"`
	UpdatePeriod     string       `json:"update_period"`
	PlaylistSort     string       `json:"playlist_sort"`
	OPML             *bool        `json:"opml,omitempty"`
	PrivateFeed      bool         `json:"private_feed"`
	KeepLast         int          `json:"keep_last"`
	Language         string       `json:"language,omitempty"`
	Filters          feed.Filters `json:"filters"`
	Custom           feed.Custom  `json:"custom"`
	YouTubeDLArgs    []string     `json:"youtube_dl_args"`
	FilenameTemplate string       `json:"filename_template"`
}

type UpdateTokensRequest struct {
	Provider string   `json:"provider"`
	Tokens   []string `json:"tokens"`
	Append   bool     `json:"append,omitempty"`
}

const maxBackupUploadSize = 512 << 20

type ChangePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

type UpdateSecurityRequest struct {
	MaxAttempts int    `json:"max_attempts"`
	Window      string `json:"window"`
	Lockout     string `json:"lockout"`
}

type FeedPreviewRequest struct {
	URL string `json:"url"`
}

type FeedPreviewResponse struct {
	Provider      string     `json:"provider"`
	Title         string     `json:"title"`
	Description   string     `json:"description"`
	Author        string     `json:"author"`
	CoverArt      string     `json:"cover_art"`
	TotalEpisodes int        `json:"total_episodes"`
	TotalBytes    int64      `json:"total_bytes"`
	LatestPubDate *time.Time `json:"latest_pub_date,omitempty"`
}

func (s *Server) registerAPIRoutes(mux *http.ServeMux) {
	mux.Handle("/api/v1/personal-feeds", s.authMiddleware(http.HandlerFunc(s.handlePersonalFeeds)))
	mux.Handle("/api/v1/personal-feeds/", s.authMiddleware(http.HandlerFunc(s.handlePersonalFeeds)))
	mux.HandleFunc("/api/v1/auth/status", s.handleAuthStatus)
	mux.HandleFunc("/api/v1/auth/login", s.handleAuthLogin)
	mux.HandleFunc("/api/v1/auth/logout", s.handleAuthLogout)
	mux.Handle("/api/v1/auth/password", s.authMiddleware(http.HandlerFunc(s.handleAuthPassword)))
	mux.Handle("/api/v1/auth/security", s.authMiddleware(http.HandlerFunc(s.handleAuthSecurity)))

	mux.Handle("/api/v1/feeds/status", s.authMiddleware(http.HandlerFunc(s.handleFeedsStatus)))
	mux.Handle("/api/v1/feeds", s.authMiddleware(http.HandlerFunc(s.handleFeeds)))
	mux.Handle("/api/v1/feeds/", s.authMiddleware(http.HandlerFunc(s.handleFeedSubroutes)))

	mux.Handle("/api/v1/tokens", s.authMiddleware(http.HandlerFunc(s.handleTokens)))
	mux.Handle("/api/v1/system/update", s.authMiddleware(http.HandlerFunc(s.handlePodsyncUpdate)))
	mux.Handle("/api/v1/system/backup/export", s.authMiddleware(http.HandlerFunc(s.handleSystemBackupExport)))
	mux.Handle("/api/v1/system/backup/inspect", s.authMiddleware(http.HandlerFunc(s.handleSystemBackupInspect)))
	mux.Handle("/api/v1/system/backup/restore", s.authMiddleware(http.HandlerFunc(s.handleSystemBackupRestore)))
	mux.Handle("/api/v1/system", s.authMiddleware(http.HandlerFunc(s.handleSystem)))
	mux.Handle("/api/v1/system/errors", s.authMiddleware(http.HandlerFunc(s.handleSystemErrors)))
	mux.Handle("/api/v1/system/recent", s.authMiddleware(http.HandlerFunc(s.handleSystemRecent)))
	mux.Handle("/api/v1/system/status", s.authMiddleware(http.HandlerFunc(s.handleSystemStatus)))
	mux.Handle("/api/v1/system/logs", s.authMiddleware(http.HandlerFunc(s.handleSystemLogs)))
	mux.Handle("/api/v1/system/logs/level", s.authMiddleware(http.HandlerFunc(s.handleSystemLogLevel)))
	mux.Handle("/api/v1/system/logs/download", s.authMiddleware(http.HandlerFunc(s.handleSystemLogsDownload)))

	mux.Handle("/api/v1/downloader", s.authMiddleware(http.HandlerFunc(s.handleDownloader)))
	mux.Handle("/api/v1/downloader/cookies", s.authMiddleware(http.HandlerFunc(s.handleCookies)))
	mux.Handle("/api/v1/downloader/test", s.authMiddleware(http.HandlerFunc(s.handleDownloaderTest)))
	mux.Handle("/api/v1/network", s.authMiddleware(http.HandlerFunc(s.handleNetwork)))
	mux.Handle("/api/v1/mcp/config", s.authMiddleware(http.HandlerFunc(s.handleMCPConfig)))

	mux.Handle("/api/v1/audiobooks/dirs", s.authMiddleware(http.HandlerFunc(s.handleAudiobookDirs)))
	mux.Handle("/api/v1/audiobooks/unlinked", s.authMiddleware(http.HandlerFunc(s.handleAudiobookDirs)))
	mux.Handle("/api/v1/audiobooks/import", s.authMiddleware(http.HandlerFunc(s.handleAudiobookImport)))
	mux.Handle("/api/v1/audiobooks/upload", s.authMiddleware(http.HandlerFunc(s.handleAudiobookUpload)))
	mux.Handle("/api/v1/audiobooks/config", s.authMiddleware(http.HandlerFunc(s.handleAudiobookConfig)))
	mux.Handle("/api/v1/covers/upload", s.authMiddleware(http.HandlerFunc(s.handleCoverUpload)))
	mux.Handle("/api/v1/feeds/preview", s.authMiddleware(http.HandlerFunc(s.handleFeedPreview)))
}

func cleanToken(providerStr, tok string) string {
	return feed.CleanToken(providerStr, tok)
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if data != nil {
		if err := json.NewEncoder(w).Encode(data); err != nil {
			log.WithError(err).Error("failed to encode JSON response")
		}
	}
}

func (s *Server) writeError(w http.ResponseWriter, status int, message string) {
	s.writeJSON(w, status, map[string]string{"error": message})
}

func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	authenticated := false
	username := ""

	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		if user, ok := s.sessions.ValidateToken(cookie.Value); ok {
			authenticated = true
			username = user
		}
	}

	if !authenticated {
		if u, p, ok := r.BasicAuth(); ok {
			ip := getClientIP(r)
			if retryAfter, blocked := s.rateLimiter.IsBlocked(ip, u); blocked {
				sec := int(retryAfter.Seconds())
				w.Header().Set("Retry-After", strconv.Itoa(sec))
				s.writeError(w, http.StatusTooManyRequests, fmt.Sprintf("Too many failed login attempts. Retry after %d seconds.", sec))
				return
			}
			if s.checkCredentials(u, p) {
				s.rateLimiter.RecordSuccess(ip, u)
				authenticated = true
				username = u
			} else {
				s.rateLimiter.RecordFailure(ip, u)
			}
		}
	}

	rateLimitInfo := map[string]interface{}{
		"max_attempts": 5,
		"window":       "5m",
		"lockout":      "5m",
	}
	if s.rateLimiter != nil {
		rateLimitInfo["max_attempts"] = s.rateLimiter.MaxAttempts()
		rateLimitInfo["window"] = formatConfigDuration(s.rateLimiter.Window())
		rateLimitInfo["lockout"] = formatConfigDuration(s.rateLimiter.Lockout())
	}

	s.cfgMu.RLock()
	adminEnabled := s.cfg.Admin.Enabled
	s.cfgMu.RUnlock()

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"enabled":       adminEnabled,
		"authenticated": authenticated,
		"username":      username,
		"rate_limit":    rateLimitInfo,
	})
}

func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ip := getClientIP(r)
	if retryAfter, blocked := s.rateLimiter.IsBlocked(ip, req.Username); blocked {
		sec := int(retryAfter.Seconds())
		w.Header().Set("Retry-After", strconv.Itoa(sec))
		s.writeError(w, http.StatusTooManyRequests, fmt.Sprintf("Too many failed login attempts. Retry after %d seconds.", sec))
		return
	}

	if !s.checkCredentials(req.Username, req.Password) {
		s.rateLimiter.RecordFailure(ip, req.Username)
		s.writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}

	s.rateLimiter.RecordSuccess(ip, req.Username)

	token, err := s.sessions.CreateSession(req.Username)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to create session")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  time.Now().Add(sessionTTL),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"username": req.Username,
	})
}

func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		s.sessions.RevokeToken(cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	s.writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (s *Server) handleAuthPassword(w http.ResponseWriter, r *http.Request) {
	if s.adminMgr == nil {
		s.writeError(w, http.StatusServiceUnavailable, "admin manager not initialized")
		return
	}

	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req ChangePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	s.adminUpdateMu.Lock()
	defer s.adminUpdateMu.Unlock()

	if err := s.adminMgr.ChangePassword(r.Context(), req.OldPassword, req.NewPassword); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Update server in-memory credentials immediately
	s.UpdateAdminPassword(req.NewPassword)

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Password changed successfully",
	})
}

func (s *Server) handleAuthSecurity(w http.ResponseWriter, r *http.Request) {
	if s.adminMgr == nil {
		s.writeError(w, http.StatusServiceUnavailable, "admin manager not initialized")
		return
	}

	switch r.Method {
	case http.MethodGet:
		rateLimitInfo := map[string]interface{}{
			"max_attempts": 5,
			"window":       "5m",
			"lockout":      "5m",
		}
		if s.rateLimiter != nil {
			rateLimitInfo["max_attempts"] = s.rateLimiter.MaxAttempts()
			rateLimitInfo["window"] = formatConfigDuration(s.rateLimiter.Window())
			rateLimitInfo["lockout"] = formatConfigDuration(s.rateLimiter.Lockout())
		}
		s.writeJSON(w, http.StatusOK, rateLimitInfo)

	case http.MethodPost, http.MethodPut:
		var req UpdateSecurityRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		if req.MaxAttempts <= 0 {
			req.MaxAttempts = 5
		}
		if req.MaxAttempts > 100 {
			s.writeError(w, http.StatusBadRequest, "max_attempts must be between 1 and 100")
			return
		}

		windowDur, err := time.ParseDuration(req.Window)
		if err != nil || windowDur < 10*time.Second {
			s.writeError(w, http.StatusBadRequest, "invalid window duration (e.g. 1m, 5m, 15m, minimum 10s)")
			return
		}

		lockoutDur, err := time.ParseDuration(req.Lockout)
		if err != nil || lockoutDur < 10*time.Second {
			s.writeError(w, http.StatusBadRequest, "invalid lockout duration (e.g. 1m, 5m, 15m, minimum 10s)")
			return
		}

		windowStr := req.Window
		lockoutStr := req.Lockout

		s.adminUpdateMu.Lock()
		defer s.adminUpdateMu.Unlock()

		if err := s.adminMgr.UpdateAdminSecurity(r.Context(), req.MaxAttempts, windowStr, lockoutStr); err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		s.UpdateSecurityConfig(req.MaxAttempts, windowDur, lockoutDur)

		s.writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"rate_limit": map[string]interface{}{
				"max_attempts": req.MaxAttempts,
				"window":       windowStr,
				"lockout":      lockoutStr,
			},
		})

	default:
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}
