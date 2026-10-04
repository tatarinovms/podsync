package web

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/mxpv/podsync/pkg/builder"
	"github.com/mxpv/podsync/pkg/feed"
	"github.com/mxpv/podsync/pkg/model"
)

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type AddFeedRequest struct {
	ID           string `json:"id"`
	URL          string `json:"url"`
	Format       string `json:"format"`
	Quality      string `json:"quality"`
	PageSize     int    `json:"page_size"`
	CronSchedule string `json:"cron_schedule"`
	UpdatePeriod string `json:"update_period"`
	KeepLast     int    `json:"keep_last"`
	Filters      struct {
		Title    string `json:"title"`
		NotTitle string `json:"not_title"`
	} `json:"filters"`
}

type UpdateTokensRequest struct {
	Provider string   `json:"provider"`
	Tokens   []string `json:"tokens"`
}

func (s *Server) registerAPIRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/auth/status", s.handleAuthStatus)
	mux.HandleFunc("/api/v1/auth/login", s.handleAuthLogin)
	mux.HandleFunc("/api/v1/auth/logout", s.handleAuthLogout)

	mux.HandleFunc("/api/v1/feeds", s.handleFeeds)
	mux.HandleFunc("/api/v1/feeds/", s.handleFeedSubroutes)

	mux.HandleFunc("/api/v1/tokens", s.handleTokens)
	mux.HandleFunc("/api/v1/system", s.handleSystem)
	mux.Handle("/api/v1/downloader", s.authMiddleware(http.HandlerFunc(s.handleDownloader)))
	mux.Handle("/api/v1/downloader/cookies", s.authMiddleware(http.HandlerFunc(s.handleCookies)))
	mux.Handle("/api/v1/downloader/test", s.authMiddleware(http.HandlerFunc(s.handleDownloaderTest)))
	mux.Handle("/api/v1/mcp/config", s.authMiddleware(http.HandlerFunc(s.handleMCPConfig)))
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
		if u, p, ok := r.BasicAuth(); ok && s.checkCredentials(u, p) {
			authenticated = true
			username = u
		}
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"enabled":       s.cfg.Admin.Enabled,
		"authenticated": authenticated,
		"username":      username,
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

	if !s.checkCredentials(req.Username, req.Password) {
		s.writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}

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

		feedCfg := &feed.Config{
			ID:           feedID,
			URL:          req.URL,
			Format:       model.Format(req.Format),
			Quality:      model.Quality(req.Quality),
			PageSize:     req.PageSize,
			CronSchedule: req.CronSchedule,
		}

		if feedCfg.Format == "" {
			feedCfg.Format = model.FormatVideo
		}
		if feedCfg.Quality == "" {
			feedCfg.Quality = model.QualityHigh
		}
		if feedCfg.PageSize <= 0 {
			feedCfg.PageSize = 20
		}
		if req.KeepLast > 0 {
			feedCfg.Clean = &feed.Cleanup{
				KeepLast: req.KeepLast,
			}
		}
		if req.Filters.Title != "" || req.Filters.NotTitle != "" {
			feedCfg.Filters = feed.Filters{
				Title:    req.Filters.Title,
				NotTitle: req.Filters.NotTitle,
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
			summary, episodes, err := s.adminMgr.GetFeedDetail(r.Context(), feedID)
			if err != nil {
				s.writeError(w, http.StatusNotFound, err.Error())
				return
			}
			s.writeJSON(w, http.StatusOK, map[string]interface{}{
				"feed":     summary,
				"episodes": episodes,
			})

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
		if err := s.adminMgr.UpdateTokens(r.Context(), req.Provider, req.Tokens); err != nil {
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

	authStr := fmt.Sprintf("%s:%s", s.cfg.Admin.Username, s.cfg.Admin.Password)
	basicAuthHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(authStr))

	configPath := s.cfg.Admin.ConfigPath
	if configPath == "" {
		configPath = "config.toml"
	}
	if absPath, err := filepath.Abs(configPath); err == nil {
		configPath = absPath
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"username":          s.cfg.Admin.Username,
		"password":          s.cfg.Admin.Password,
		"basic_auth_header": basicAuthHeader,
		"binary_path":       execPath,
		"config_path":       configPath,
		"port":              s.cfg.Port,
		"hostname":          s.cfg.Hostname,
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
