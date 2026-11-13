// Local audiobook library REST handlers.
package web

import (
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/mxpv/podsync/pkg/feed"
)

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
