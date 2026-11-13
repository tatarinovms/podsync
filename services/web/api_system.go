// System diagnostics, backup, logs, downloader and network handlers.
package web

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	log "github.com/sirupsen/logrus"
)

func (s *Server) handleSystemBackupExport(w http.ResponseWriter, r *http.Request) {
	if s.adminMgr == nil {
		s.writeError(w, http.StatusServiceUnavailable, "admin manager not initialized")
		return
	}
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	includeSecrets := r.URL.Query().Get("include_secrets") != "false"
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="podsync-backup-%s.tar.gz"`, time.Now().UTC().Format("20060102-150405")))
	if _, err := s.adminMgr.CreateSystemBackup(r.Context(), includeSecrets, w); err != nil {
		log.WithError(err).Error("backup export failed")
	}
}

func (s *Server) backupUpload(w http.ResponseWriter, r *http.Request) (multipart.File, *multipart.FileHeader, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBackupUploadSize)
	if err := r.ParseMultipartForm(maxBackupUploadSize); err != nil {
		return nil, nil, err
	}
	return r.FormFile("file")
}

func (s *Server) handleSystemBackupInspect(w http.ResponseWriter, r *http.Request) {
	if s.adminMgr == nil {
		s.writeError(w, http.StatusServiceUnavailable, "admin manager not initialized")
		return
	}
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	f, _, err := s.backupUpload(w, r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "backup file is required and must be under 512 MiB")
		return
	}
	defer f.Close()
	info, err := s.adminMgr.InspectSystemBackup(r.Context(), f)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleSystemBackupRestore(w http.ResponseWriter, r *http.Request) {
	if s.adminMgr == nil {
		s.writeError(w, http.StatusServiceUnavailable, "admin manager not initialized")
		return
	}
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	f, _, err := s.backupUpload(w, r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "backup file is required and must be under 512 MiB")
		return
	}
	defer f.Close()
	preserve := r.FormValue("preserve_paths") != "false"
	secrets := r.FormValue("restore_secrets") != "false"
	info, err := s.adminMgr.RestoreSystemBackup(r.Context(), f, BackupRestoreOptions{PreservePaths: preserve, RestoreSecrets: secrets})
	if err != nil {
		s.writeError(w, http.StatusConflict, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, info)
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

func (s *Server) handlePodsyncUpdate(w http.ResponseWriter, r *http.Request) {
	if s.adminMgr == nil {
		s.writeError(w, http.StatusServiceUnavailable, "admin manager not initialized")
		return
	}

	switch r.Method {
	case http.MethodGet:
		info, err := s.adminMgr.GetPodsyncUpdate(r.Context())
		if err != nil {
			s.writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, info)
	case http.MethodPost:
		info, err := s.adminMgr.ApplyPodsyncUpdate(r.Context())
		if err != nil {
			s.writeError(w, http.StatusConflict, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, info)
	default:
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
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

func (s *Server) handleNetwork(w http.ResponseWriter, r *http.Request) {
	if s.adminMgr == nil {
		s.writeError(w, http.StatusServiceUnavailable, "admin manager not initialized")
		return
	}

	switch r.Method {
	case http.MethodGet:
		cfg, err := s.adminMgr.GetNetworkConfig(r.Context())
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, cfg)
	case http.MethodPost:
		var req NetworkConfigUpdate
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if req.DNSServers == nil {
			s.writeError(w, http.StatusBadRequest, "dns_servers is required")
			return
		}
		if err := s.adminMgr.UpdateNetworkConfig(r.Context(), &req); err != nil {
			s.writeError(w, http.StatusBadRequest, err.Error())
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
