package web

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/mxpv/podsync/pkg/builder"
	"github.com/mxpv/podsync/pkg/feed"
	"github.com/mxpv/podsync/pkg/model"
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
	tok = strings.TrimSpace(tok)
	tok = strings.Trim(tok, "\"'")
	if strings.EqualFold(providerStr, "vkvideo") || strings.EqualFold(providerStr, "vk") || strings.Contains(tok, "access_token=") {
		if idx := strings.Index(tok, "access_token="); idx != -1 {
			sub := tok[idx+len("access_token="):]
			if end := strings.IndexAny(sub, " &;\n\r#"); end != -1 {
				return sub[:end]
			}
			return sub
		}
	}
	return tok
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

type ChangePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
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

type UpdateSecurityRequest struct {
	MaxAttempts int    `json:"max_attempts"`
	Window      string `json:"window"`
	Lockout     string `json:"lockout"`
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

func (s *Server) handleFeedsStatus(w http.ResponseWriter, r *http.Request) {
	if s.adminMgr == nil {
		s.writeError(w, http.StatusServiceUnavailable, "admin manager not initialized")
		return
	}

	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	statuses, err := s.adminMgr.GetFeedsStatus(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, statuses)
}

func (s *Server) handleFeeds(w http.ResponseWriter, r *http.Request) {
	if s.adminMgr == nil {
		s.writeError(w, http.StatusServiceUnavailable, "admin manager not initialized")
		return
	}

	switch r.Method {
	case http.MethodGet:
		feeds, err := s.adminMgr.ListFeeds(r.Context())
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, feeds)

	case http.MethodPost:
		var req AddFeedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.writeError(w, http.StatusBadRequest, "invalid request payload")
			return
		}

		req.URL = strings.TrimSpace(req.URL)
		if req.URL == "" {
			s.writeError(w, http.StatusBadRequest, "url is required")
			return
		}

		info, err := builder.ParseURL(req.URL)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, "unsupported or invalid feed URL: "+err.Error())
			return
		}

		feedID := strings.TrimSpace(req.ID)
		if feedID == "" {
			// Auto generate a safe ID from item ID or URL
			feedID = strings.ToUpper(strings.ReplaceAll(info.ItemID, "-", "_"))
		}
		if feedID == "" {
			s.writeError(w, http.StatusBadRequest, "could not generate feed id, please specify one")
			return
		}
		if !validFeedIDRegex.MatchString(feedID) {
			s.writeError(w, http.StatusBadRequest, "feed id must contain only English letters (a-z, A-Z), numbers, dashes, and underscores")
			return
		}

		if req.Language != "" && req.Custom.Language == "" {
			req.Custom.Language = strings.TrimSpace(req.Language)
		}
		feedCfg := &feed.Config{
			ID:               feedID,
			URL:              req.URL,
			Format:           model.Format(req.Format),
			Quality:          model.Quality(req.Quality),
			MaxHeight:        req.MaxHeight,
			PageSize:         req.PageSize,
			CronSchedule:     req.CronSchedule,
			PlaylistSort:     model.Sorting(req.PlaylistSort),
			OPML:             req.OPML,
			PrivateFeed:      req.PrivateFeed,
			Filters:          req.Filters,
			Custom:           req.Custom,
			YouTubeDLArgs:    req.YouTubeDLArgs,
			FilenameTemplate: req.FilenameTemplate,
		}

		if feedCfg.Format == "" {
			feedCfg.Format = model.FormatAudio
		}
		if feedCfg.Quality == "" {
			feedCfg.Quality = model.QualityHigh
		}
		if feedCfg.PageSize <= 0 {
			feedCfg.PageSize = 50
		}
		if req.KeepLast > 0 {
			feedCfg.Clean = &feed.Cleanup{
				KeepLast: req.KeepLast,
			}
		}

		if err := s.adminMgr.AddFeed(r.Context(), feedCfg); err != nil {
			s.writeError(w, http.StatusInternalServerError, "failed to add feed: "+err.Error())
			return
		}

		s.writeJSON(w, http.StatusCreated, map[string]interface{}{
			"success": true,
			"feed_id": feedID,
		})

	default:
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleFeedSubroutes(w http.ResponseWriter, r *http.Request) {
	if s.adminMgr == nil {
		s.writeError(w, http.StatusServiceUnavailable, "admin manager not initialized")
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/v1/feeds/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		s.writeError(w, http.StatusBadRequest, "feed id is required")
		return
	}

	// Feed status: /api/v1/feeds/status
	if len(parts) == 1 && parts[0] == "status" {
		s.handleFeedsStatus(w, r)
		return
	}

	// Update all feeds: /api/v1/feeds/update or /api/v1/feeds/update-all
	if len(parts) == 1 && (parts[0] == "update" || parts[0] == "update-all") {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		queued, err := s.adminMgr.TriggerUpdateAll(r.Context())
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]interface{}{
			"queued": true,
			"count":  len(queued),
			"feeds":  queued,
		})
		return
	}

	feedID := parts[0]

	// 1. /api/v1/feeds/{id}/update
	if len(parts) == 2 && parts[1] == "update" {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if err := s.adminMgr.TriggerUpdate(r.Context(), feedID); err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]bool{"queued": true})
		return
	}

	// 1.5. /api/v1/feeds/{id}/cover
	if len(parts) == 2 && parts[1] == "cover" {
		s.handleFeedCoverUpload(w, r, feedID)
		return
	}

	// 1.8. /api/v1/feeds/{id}/episodes
	if len(parts) == 2 && parts[1] == "episodes" {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		q := r.URL.Query()
		limit, _ := strconv.Atoi(q.Get("limit"))
		offset, _ := strconv.Atoi(q.Get("offset"))
		query := EpisodeQuery{
			Limit:    limit,
			Offset:   offset,
			Status:   q.Get("status"),
			Search:   q.Get("q"),
			Sort:     q.Get("sort"),
			Duration: q.Get("duration"),
		}
		page, err := s.adminMgr.GetFeedEpisodes(r.Context(), feedID, query)
		if err != nil {
			s.writeError(w, http.StatusNotFound, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, page)
		return
	}

	// 2. /api/v1/feeds/{id}/episodes/{episode_id}/retry
	if len(parts) == 4 && parts[1] == "episodes" && parts[3] == "retry" {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		episodeID := parts[2]
		if err := s.adminMgr.RetryEpisode(r.Context(), feedID, episodeID); err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]bool{"success": true})
		return
	}

	// 3. /api/v1/feeds/{id}
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			limitStr := r.URL.Query().Get("limit")
			if limitStr != "" {
				limit, _ := strconv.Atoi(limitStr)
				offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
				query := EpisodeQuery{
					Limit:    limit,
					Offset:   offset,
					Status:   r.URL.Query().Get("status"),
					Search:   r.URL.Query().Get("q"),
					Sort:     r.URL.Query().Get("sort"),
					Duration: r.URL.Query().Get("duration"),
				}
				page, err := s.adminMgr.GetFeedEpisodes(r.Context(), feedID, query)
				if err != nil {
					s.writeError(w, http.StatusNotFound, err.Error())
					return
				}
				summary, _, err := s.adminMgr.GetFeedDetail(r.Context(), feedID)
				if err != nil {
					s.writeError(w, http.StatusNotFound, err.Error())
					return
				}
				s.writeJSON(w, http.StatusOK, map[string]interface{}{
					"feed":        summary,
					"episodes":    page.Episodes,
					"total":       page.Total,
					"matching":    page.Matching,
					"offset":      page.Offset,
					"limit":       page.Limit,
					"has_more":    page.HasMore,
					"next_offset": page.NextOffset,
				})
				return
			}
			summary, episodes, err := s.adminMgr.GetFeedDetail(r.Context(), feedID)
			if err != nil {
				s.writeError(w, http.StatusNotFound, err.Error())
				return
			}
			s.writeJSON(w, http.StatusOK, map[string]interface{}{
				"feed":     summary,
				"episodes": episodes,
			})

		case http.MethodPut, http.MethodPatch, http.MethodPost:
			var req FeedConfigUpdate
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				s.writeError(w, http.StatusBadRequest, "invalid request")
				return
			}
			if err := s.adminMgr.UpdateFeed(r.Context(), feedID, &req); err != nil {
				s.writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			s.writeJSON(w, http.StatusOK, map[string]bool{"success": true})

		case http.MethodDelete:
			deleteFiles := r.URL.Query().Get("delete_files") == "true"
			if err := s.adminMgr.DeleteFeed(r.Context(), feedID, deleteFiles); err != nil {
				s.writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			s.writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})

		default:
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}

	s.writeError(w, http.StatusNotFound, "route not found")
}

func (s *Server) handleTokens(w http.ResponseWriter, r *http.Request) {
	if s.adminMgr == nil {
		s.writeError(w, http.StatusServiceUnavailable, "admin manager not initialized")
		return
	}

	switch r.Method {
	case http.MethodGet:
		tokens := s.adminMgr.GetTokens(r.Context())
		s.writeJSON(w, http.StatusOK, tokens)

	case http.MethodPost:
		var req UpdateTokensRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.writeError(w, http.StatusBadRequest, "invalid request")
			return
		}
		if req.Provider == "" {
			s.writeError(w, http.StatusBadRequest, "provider is required")
			return
		}
		for i, tok := range req.Tokens {
			req.Tokens[i] = cleanToken(req.Provider, tok)
		}
		if req.Append {
			if err := s.adminMgr.AddTokens(r.Context(), req.Provider, req.Tokens); err != nil {
				s.writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
		} else {
			if err := s.adminMgr.UpdateTokens(r.Context(), req.Provider, req.Tokens); err != nil {
				s.writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		s.writeJSON(w, http.StatusOK, map[string]bool{"success": true})

	case http.MethodDelete:
		provider := r.URL.Query().Get("provider")
		if provider == "" {
			s.writeError(w, http.StatusBadRequest, "provider query parameter is required")
			return
		}
		idx := -1
		if indexStr := r.URL.Query().Get("index"); indexStr != "" {
			var err error
			idx, err = strconv.Atoi(indexStr)
			if err != nil {
				s.writeError(w, http.StatusBadRequest, "invalid index parameter")
				return
			}
		}
		if err := s.adminMgr.DeleteToken(r.Context(), provider, idx); err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]bool{"success": true})

	default:
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleSystem(w http.ResponseWriter, r *http.Request) {
	if s.adminMgr == nil {
		s.writeError(w, http.StatusServiceUnavailable, "admin manager not initialized")
		return
	}

	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	stats, err := s.adminMgr.GetSystemStats(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, stats)
}

func (s *Server) handleMCPConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	execPath, err := os.Executable()
	if err != nil {
		execPath = "podsync"
	}

	s.cfgMu.RLock()
	username := s.cfg.Admin.Username
	password := s.cfg.Admin.Password
	configPath := s.cfg.Admin.ConfigPath
	port := s.cfg.Port
	hostname := s.cfg.Hostname
	s.cfgMu.RUnlock()

	authStr := fmt.Sprintf("%s:%s", username, password)
	basicAuthHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(authStr))

	if configPath == "" {
		configPath = "config.toml"
	}
	if absPath, err := filepath.Abs(configPath); err == nil {
		configPath = absPath
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"username":          username,
		"password":          password,
		"basic_auth_header": basicAuthHeader,
		"binary_path":       execPath,
		"config_path":       configPath,
		"port":              port,
		"hostname":          hostname,
		"mcp_enabled":       true,
	})
}

func (s *Server) handleDownloader(w http.ResponseWriter, r *http.Request) {
	if s.adminMgr == nil {
		s.writeError(w, http.StatusServiceUnavailable, "admin manager not initialized")
		return
	}

	switch r.Method {
	case http.MethodGet:
		cfg, err := s.adminMgr.GetDownloaderConfig(r.Context())
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, cfg)

	case http.MethodPost:
		var req DownloaderConfigUpdate
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if err := s.adminMgr.UpdateDownloaderConfig(r.Context(), &req); err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]bool{"success": true})

	default:
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleCookies(w http.ResponseWriter, r *http.Request) {
	if s.adminMgr == nil {
		s.writeError(w, http.StatusServiceUnavailable, "admin manager not initialized")
		return
	}

	switch r.Method {
	case http.MethodGet:
		content, err := s.adminMgr.GetCookiesContent(r.Context())
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, CookiesPayload{Content: content})

	case http.MethodPost:
		var req CookiesPayload
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if err := s.adminMgr.UpdateCookiesContent(r.Context(), req.Content); err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]bool{"success": true})

	case http.MethodDelete:
		if err := s.adminMgr.UpdateCookiesContent(r.Context(), ""); err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]bool{"success": true})

	default:
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleDownloaderTest(w http.ResponseWriter, r *http.Request) {
	if s.adminMgr == nil {
		s.writeError(w, http.StatusServiceUnavailable, "admin manager not initialized")
		return
	}

	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		URL string `json:"url"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	res, err := s.adminMgr.TestDownloader(r.Context(), req.URL)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleSystemErrors(w http.ResponseWriter, r *http.Request) {
	if s.adminMgr == nil {
		s.writeError(w, http.StatusServiceUnavailable, "admin manager not initialized")
		return
	}
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	feedID := r.URL.Query().Get("feed_id")
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if val, err := strconv.Atoi(raw); err == nil && val > 0 {
			limit = val
		}
	}

	errs, err := s.adminMgr.GetDownloadErrors(r.Context(), feedID, limit)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, errs)
}

func (s *Server) handleSystemRecent(w http.ResponseWriter, r *http.Request) {
	if s.adminMgr == nil {
		s.writeError(w, http.StatusServiceUnavailable, "admin manager not initialized")
		return
	}
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	feedID := r.URL.Query().Get("feed_id")
	limit := 10
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if val, err := strconv.Atoi(raw); err == nil && val > 0 {
			limit = val
		}
	}

	downloads, err := s.adminMgr.GetRecentDownloads(r.Context(), feedID, limit)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, downloads)
}

func (s *Server) handleSystemStatus(w http.ResponseWriter, r *http.Request) {
	if s.adminMgr == nil {
		s.writeError(w, http.StatusServiceUnavailable, "admin manager not initialized")
		return
	}
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	status, err := s.adminMgr.GetSyncStatus(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleSystemLogs(w http.ResponseWriter, r *http.Request) {
	if s.adminMgr == nil {
		s.writeError(w, http.StatusServiceUnavailable, "admin manager not initialized")
		return
	}

	switch r.Method {
	case http.MethodGet:
		limit := 200
		if raw := r.URL.Query().Get("limit"); raw != "" {
			if val, err := strconv.Atoi(raw); err == nil && val > 0 {
				limit = val
			}
		}
		var sinceID int64
		if raw := r.URL.Query().Get("since_id"); raw != "" {
			if val, err := strconv.ParseInt(raw, 10, 64); err == nil {
				sinceID = val
			}
		}

		filter := LogFilter{
			Level:   r.URL.Query().Get("level"),
			Search:  r.URL.Query().Get("search"),
			FeedID:  r.URL.Query().Get("feed_id"),
			Limit:   limit,
			SinceID: sinceID,
		}

		res, err := s.adminMgr.GetSystemLogs(r.Context(), filter)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, res)

	case http.MethodDelete:
		if err := s.adminMgr.ClearSystemLogs(r.Context()); err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})

	default:
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleSystemLogLevel(w http.ResponseWriter, r *http.Request) {
	if s.adminMgr == nil {
		s.writeError(w, http.StatusServiceUnavailable, "admin manager not initialized")
		return
	}

	switch r.Method {
	case http.MethodGet:
		level := s.adminMgr.GetLogLevel(r.Context())
		s.writeJSON(w, http.StatusOK, map[string]string{"level": level})

	case http.MethodPost:
		var req struct {
			Level string `json:"level"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if req.Level == "" {
			s.writeError(w, http.StatusBadRequest, "level is required")
			return
		}
		newLevel, err := s.adminMgr.SetLogLevel(r.Context(), req.Level)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]string{"level": newLevel})

	default:
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleSystemLogsDownload(w http.ResponseWriter, r *http.Request) {
	if s.adminMgr == nil {
		s.writeError(w, http.StatusServiceUnavailable, "admin manager not initialized")
		return
	}
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	content, filename, err := s.adminMgr.DownloadLogs(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if filename == "" {
		filename = "podsync.log"
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

func (s *Server) handleAudiobookDirs(w http.ResponseWriter, r *http.Request) {
	if s.adminMgr == nil {
		s.writeError(w, http.StatusServiceUnavailable, "admin manager not initialized")
		return
	}

	switch r.Method {
	case http.MethodGet:
		dirs, err := s.adminMgr.ListAudiobookDirs(r.Context())
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if dirs == nil {
			dirs = make([]AudiobookDirInfo, 0)
		}
		if r.URL.Query().Get("unlinked") == "true" || strings.HasSuffix(r.URL.Path, "/unlinked") {
			filtered := make([]AudiobookDirInfo, 0, len(dirs))
			for _, d := range dirs {
				if !d.Linked {
					filtered = append(filtered, d)
				}
			}
			s.writeJSON(w, http.StatusOK, filtered)
			return
		}
		s.writeJSON(w, http.StatusOK, dirs)

	case http.MethodDelete:
		dirName := r.URL.Query().Get("name")
		if dirName == "" {
			dirName = r.URL.Query().Get("dir")
		}
		if dirName == "" {
			var bodyReq struct {
				Name    string `json:"name"`
				DirName string `json:"dir_name"`
			}
			if err := json.NewDecoder(r.Body).Decode(&bodyReq); err == nil {
				if bodyReq.Name != "" {
					dirName = bodyReq.Name
				} else {
					dirName = bodyReq.DirName
				}
			}
		}
		dirName = strings.TrimSpace(dirName)
		if dirName == "" {
			s.writeError(w, http.StatusBadRequest, "directory name is required (use ?name=... or JSON body)")
			return
		}
		if err := s.adminMgr.DeleteAudiobookDir(r.Context(), dirName); err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"deleted": dirName,
		})

	default:
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleAudiobookImport(w http.ResponseWriter, r *http.Request) {
	if s.adminMgr == nil {
		s.writeError(w, http.StatusServiceUnavailable, "admin manager not initialized")
		return
	}
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req ImportAudiobookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ID != "" && !validFeedIDRegex.MatchString(strings.TrimSpace(req.ID)) {
		s.writeError(w, http.StatusBadRequest, "feed id must contain only English letters (a-z, A-Z), numbers, dashes, and underscores")
		return
	}
	feedCfg, err := s.adminMgr.ImportAudiobook(r.Context(), &req)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]interface{}{
		"success": true,
		"feed_id": feedCfg.ID,
		"feed":    feedCfg,
	})
}

func (s *Server) handleAudiobookUpload(w http.ResponseWriter, r *http.Request) {
	if s.adminMgr == nil {
		s.writeError(w, http.StatusServiceUnavailable, "admin manager not initialized")
		return
	}
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	const maxUploadSize = 1024 << 20
	if r.ContentLength > maxUploadSize {
		s.writeError(w, http.StatusRequestEntityTooLarge, "upload exceeds 1 GiB limit")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		var limitErr *http.MaxBytesError
		if errors.As(err, &limitErr) {
			s.writeError(w, http.StatusRequestEntityTooLarge, "upload exceeds 1 GiB limit")
			return
		}
		s.writeError(w, http.StatusBadRequest, "failed to parse multipart form: "+err.Error())
		return
	}

	bookDir := strings.TrimSpace(r.FormValue("book_dir"))
	if bookDir == "" {
		bookDir = strings.TrimSpace(r.FormValue("dir_name"))
	}
	if bookDir == "" {
		s.writeError(w, http.StatusBadRequest, "book_dir is required")
		return
	}

	var files []*multipart.FileHeader
	if r.MultipartForm != nil {
		if list, ok := r.MultipartForm.File["files"]; ok {
			files = append(files, list...)
		}
		if single, ok := r.MultipartForm.File["file"]; ok {
			files = append(files, single...)
		}
		if coverFiles, ok := r.MultipartForm.File["cover"]; ok {
			files = append(files, coverFiles...)
		}
	}

	if len(files) == 0 {
		s.writeError(w, http.StatusBadRequest, "no files provided in upload")
		return
	}

	savedCount := 0
	for _, fh := range files {
		file, err := fh.Open()
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, "failed to open uploaded file: "+err.Error())
			return
		}

		err = s.adminMgr.SaveAudiobookFile(bookDir, fh.Filename, file)
		file.Close()
		if err != nil {
			s.writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": "failed to save file " + fh.Filename + ": " + err.Error(), "uploaded": savedCount})
			return
		}
		savedCount++
	}

	autoImport := r.FormValue("auto_import") == "true" || r.FormValue("auto_import") == "1"
	var createdFeed *feed.Config
	if autoImport {
		id := strings.TrimSpace(r.FormValue("id"))
		if id != "" && !validFeedIDRegex.MatchString(id) {
			s.writeJSON(w, http.StatusBadRequest, map[string]interface{}{
				"error":    "feed id must contain only English letters (a-z, A-Z), numbers, dashes, and underscores",
				"uploaded": savedCount, "book_dir": bookDir, "imported": false,
			})
			return
		}
		title := strings.TrimSpace(r.FormValue("title"))
		author := strings.TrimSpace(r.FormValue("author"))
		desc := strings.TrimSpace(r.FormValue("description"))
		feedCfg, err := s.adminMgr.ImportAudiobook(r.Context(), &ImportAudiobookRequest{
			DirName:     bookDir,
			ID:          id,
			Title:       title,
			Author:      author,
			Description: desc,
		})
		if err != nil {
			s.writeJSON(w, http.StatusUnprocessableEntity, map[string]interface{}{
				"error":    "files uploaded, but import failed: " + err.Error(),
				"uploaded": savedCount, "book_dir": bookDir, "imported": false,
			})
			return
		}
		createdFeed = feedCfg
	}

	resp := map[string]interface{}{
		"success":  true,
		"uploaded": savedCount,
		"book_dir": bookDir,
	}
	if createdFeed != nil {
		resp["imported"] = true
		resp["feed_id"] = createdFeed.ID
	}
	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAudiobookConfig(w http.ResponseWriter, r *http.Request) {
	if s.adminMgr == nil {
		s.writeError(w, http.StatusServiceUnavailable, "admin manager not initialized")
		return
	}
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{
		"audiobooks_dir": s.adminMgr.GetAudiobooksDir(),
	})
}

func (s *Server) handleCoverUpload(w http.ResponseWriter, r *http.Request) {
	s.handleCoverUploadCommon(w, r, "")
}

func (s *Server) handleFeedCoverUpload(w http.ResponseWriter, r *http.Request, feedID string) {
	s.handleCoverUploadCommon(w, r, feedID)
}

func (s *Server) handleCoverUploadCommon(w http.ResponseWriter, r *http.Request, explicitFeedID string) {
	if s.adminMgr == nil {
		s.writeError(w, http.StatusServiceUnavailable, "admin manager not initialized")
		return
	}
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	const maxUploadSize = 15 << 20 // 15 MiB
	if r.ContentLength > maxUploadSize {
		s.writeError(w, http.StatusRequestEntityTooLarge, "cover image exceeds 15 MiB limit")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

	if err := r.ParseMultipartForm(10 << 20); err != nil {
		var limitErr *http.MaxBytesError
		if errors.As(err, &limitErr) {
			s.writeError(w, http.StatusRequestEntityTooLarge, "cover image exceeds 15 MiB limit")
			return
		}
		s.writeError(w, http.StatusBadRequest, "failed to parse multipart form: "+err.Error())
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		file, header, err = r.FormFile("cover")
		if err != nil {
			file, header, err = r.FormFile("image")
		}
	}
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "no image file uploaded (expected form field 'file' or 'cover')")
		return
	}
	defer file.Close()

	feedID := explicitFeedID
	if feedID == "" {
		feedID = strings.TrimSpace(r.FormValue("feed_id"))
	}

	res, err := s.adminMgr.SaveCoverImage(r.Context(), feedID, header.Filename, file)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to save cover image: "+err.Error())
		return
	}

	if feedID != "" && (r.URL.Query().Get("save") == "true" || explicitFeedID != "") {
		_ = s.adminMgr.UpdateFeed(r.Context(), feedID, &FeedConfigUpdate{
			Custom: feed.Custom{CoverArt: res.URL},
		})
	}

	s.writeJSON(w, http.StatusOK, res)
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

func (s *Server) handleFeedPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req FeedPreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	rawURL := strings.TrimSpace(req.URL)
	if rawURL == "" {
		s.writeError(w, http.StatusBadRequest, "url is required")
		return
	}

	info, err := builder.ParseURL(rawURL)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid feed URL: "+err.Error())
		return
	}

	if info.Provider == model.ProviderRSS {
		rssBuilder := builder.NewRSSBuilder()
		meta, err := rssBuilder.FetchFeed(r.Context(), rawURL)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, "failed to fetch RSS feed: "+err.Error())
			return
		}

		s.writeJSON(w, http.StatusOK, FeedPreviewResponse{
			Provider:      string(model.ProviderRSS),
			Title:         meta.Title,
			Description:   meta.Description,
			Author:        meta.Author,
			CoverArt:      meta.CoverArt,
			TotalEpisodes: meta.TotalEpisodes,
			TotalBytes:    meta.TotalBytes,
			LatestPubDate: meta.LatestPubDate,
		})
		return
	}

	s.writeJSON(w, http.StatusOK, FeedPreviewResponse{
		Provider: string(info.Provider),
	})
}
