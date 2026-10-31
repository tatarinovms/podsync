package web

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mxpv/podsync/pkg/model"
	"github.com/mxpv/podsync/pkg/personal"
)

func (s *Server) SetPersonalFeeds(service *personal.Service) { s.personal = service }

func (s *Server) personalError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, model.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, personal.ErrInvalid):
		status = http.StatusBadRequest
	case errors.Is(err, personal.ErrConflict):
		status = http.StatusConflict
	}
	s.writeError(w, status, err.Error())
}
func decodePersonal(w http.ResponseWriter, r *http.Request, out interface{}) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	var extra interface{}
	if err := dec.Decode(&extra); err != io.EOF {
		if err != nil {
			return err
		}
		return errors.New("only one JSON object is allowed")
	}
	return nil
}
func (s *Server) handlePersonalFeeds(w http.ResponseWriter, r *http.Request) {
	if s.personal == nil {
		s.writeError(w, 503, "personal feeds unavailable")
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/personal-feeds")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	var result interface{}
	var err error
	status := http.StatusOK
	badBody := func(e error) { s.writeError(w, 400, "invalid request: "+e.Error()) }
	if path == "" || path == "/" {
		switch r.Method {
		case http.MethodGet:
			result, err = s.personal.List()
		case http.MethodPost:
			var p personal.Patch
			if e := decodePersonal(w, r, &p); e != nil {
				badBody(e)
				return
			}
			// Generated personal IDs occupy a reserved namespace; existing channel IDs
			// are also checked explicitly for installations predating this feature.
			reserved := map[string]bool{}
			if s.adminMgr != nil {
				feeds, e := s.adminMgr.ListFeeds(r.Context())
				if e != nil {
					s.personalError(w, e)
					return
				}
				for _, f := range feeds {
					reserved[f.ID] = true
				}
			}
			result, err = s.personal.Create(r.Context(), p, reserved)
			status = http.StatusCreated
		default:
			s.writeError(w, 405, "method not allowed")
			return
		}
	} else if len(parts) == 1 {
		id := parts[0]
		switch r.Method {
		case http.MethodGet:
			result, err = s.personal.Get(id)
		case http.MethodPatch:
			var p personal.Patch
			if e := decodePersonal(w, r, &p); e != nil {
				badBody(e)
				return
			}
			result, err = s.personal.Patch(r.Context(), id, p)
		case http.MethodDelete:
			if r.URL.Query().Get("confirm") != "true" {
				s.writeError(w, 400, "deletion requires confirm=true")
				return
			}
			err = s.personal.Delete(r.Context(), id, r.URL.Query().Get("delete_files") == "true")
			status = http.StatusNoContent
		default:
			s.writeError(w, 405, "method not allowed")
			return
		}
	} else if len(parts) == 2 && parts[1] == "episodes" {
		switch r.Method {
		case http.MethodGet:
			var d *personal.Detail
			d, err = s.personal.Get(parts[0])
			if err == nil {
				result = d.Episodes
			}
		case http.MethodPost:
			var req struct {
				URL       string `json:"url"`
				FilePath  string `json:"file_path"`
				Title     string `json:"title"`
				Format    string `json:"format"`
				Quality   string `json:"quality"`
				MaxHeight int    `json:"max_height"`
			}
			if e := decodePersonal(w, r, &req); e != nil {
				badBody(e)
				return
			}
			if req.FilePath != "" {
				result, err = s.personal.AddPath(r.Context(), parts[0], req.FilePath, req.Title, req.Format, req.Quality, req.MaxHeight)
			} else {
				result, err = s.personal.AddURL(r.Context(), parts[0], req.URL, req.Title, req.Format, req.Quality, req.MaxHeight)
			}
			status = http.StatusAccepted
		default:
			s.writeError(w, 405, "method not allowed")
			return
		}
	} else if len(parts) == 2 && parts[1] == "quick-add" {
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			w.Header().Set("Allow", "GET, POST")
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		id := parts[0]
		authed := false
		token := strings.TrimSpace(r.URL.Query().Get("token"))
		if token == "" {
			authHeader := r.Header.Get("Authorization")
			if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
				token = strings.TrimSpace(authHeader[7:])
			} else {
				token = r.Header.Get("X-Quick-Add-Token")
			}
		}

		if token != "" && s.personal.ValidateQuickAddToken(id, token) {
			authed = true
		} else if s.isRequestAuthenticated(r) {
			authed = true
		}

		if !authed {
			s.writeError(w, http.StatusUnauthorized, "invalid or missing quick-add authorization token")
			return
		}

		var targetURL, title, format, quality string
		targetURL = strings.TrimSpace(r.URL.Query().Get("url"))
		title = strings.TrimSpace(r.URL.Query().Get("title"))
		format = strings.TrimSpace(r.URL.Query().Get("format"))
		quality = strings.TrimSpace(r.URL.Query().Get("quality"))

		if r.Method == http.MethodPost && r.Body != nil && r.ContentLength > 0 {
			var req struct {
				URL     string `json:"url"`
				Title   string `json:"title"`
				Format  string `json:"format"`
				Quality string `json:"quality"`
			}
			if e := decodePersonal(w, r, &req); e == nil {
				if req.URL != "" {
					targetURL = req.URL
				}
				if req.Title != "" {
					title = req.Title
				}
				if req.Format != "" {
					format = req.Format
				}
				if req.Quality != "" {
					quality = req.Quality
				}
			}
		}

		if targetURL == "" {
			s.writeError(w, http.StatusBadRequest, "url parameter is required")
			return
		}

		res, err := s.personal.AddURL(r.Context(), id, targetURL, title, format, quality, 0)
		if err != nil {
			s.personalError(w, err)
			return
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":     "queued",
			"feed_id":    id,
			"episode_id": res.ID,
			"title":      res.Title,
			"message":    "Episode queued for background download",
		})
		return
	} else if len(parts) == 2 && parts[1] == "upload" {
		if r.Method != http.MethodPost {
			s.writeError(w, 405, "method not allowed")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4<<30)
		if e := r.ParseMultipartForm(8 << 20); e != nil {
			if r.MultipartForm != nil {
				_ = r.MultipartForm.RemoveAll()
			}
			s.writeError(w, 413, "invalid multipart upload or file exceeds 4 GiB")
			return
		}
		defer r.MultipartForm.RemoveAll()
		file, header, e := r.FormFile("file")
		if e != nil {
			badBody(e)
			return
		}
		defer file.Close()
		name := header.Filename
		title := r.FormValue("title")
		if strings.TrimSpace(title) != "" {
			name = strings.TrimSpace(title) + strings.ToLower(filepath.Ext(header.Filename))
		}
		format := r.FormValue("format")
		quality := r.FormValue("quality")
		maxHeight, _ := strconv.Atoi(r.FormValue("max_height"))
		result, err = s.personal.Upload(r.Context(), parts[0], name, file, format, quality, maxHeight)
		status = http.StatusAccepted
	} else if len(parts) == 3 && parts[1] == "episodes" {
		switch r.Method {
		case http.MethodDelete:
			if r.URL.Query().Get("confirm") != "true" {
				s.writeError(w, 400, "deletion requires confirm=true")
				return
			}
			err = s.personal.DeleteEpisode(r.Context(), parts[0], parts[2], r.URL.Query().Get("delete_files") == "true")
			status = http.StatusNoContent
		case http.MethodPatch, http.MethodPut:
			var req struct {
				Title string `json:"title"`
			}
			if e := decodePersonal(w, r, &req); e != nil {
				badBody(e)
				return
			}
			result, err = s.personal.RenameEpisode(r.Context(), parts[0], parts[2], req.Title)
			status = http.StatusOK
		default:
			s.writeError(w, 405, "method not allowed")
			return
		}
	} else if len(parts) == 4 && parts[1] == "episodes" {
		if r.Method != http.MethodPost {
			s.writeError(w, 405, "method not allowed")
			return
		}
		switch parts[3] {
		case "copy", "move":
			var req struct {
				TargetFeedID string `json:"target_feed_id"`
			}
			if e := decodePersonal(w, r, &req); e != nil {
				badBody(e)
				return
			}
			result, err = s.personal.Transfer(r.Context(), parts[0], parts[2], req.TargetFeedID, parts[3] == "move")
		case "retry":
			err = s.personal.Retry(r.Context(), parts[0], parts[2])
			result = map[string]bool{"queued": true}
			status = http.StatusAccepted
		case "rename":
			var req struct {
				Title string `json:"title"`
			}
			if e := decodePersonal(w, r, &req); e != nil {
				badBody(e)
				return
			}
			result, err = s.personal.RenameEpisode(r.Context(), parts[0], parts[2], req.Title)
			status = http.StatusOK
		default:
			s.writeError(w, 404, "unknown episode action")
			return
		}
	} else {
		s.writeError(w, 404, "not found")
		return
	}
	if err != nil {
		s.personalError(w, err)
		return
	}
	if status == http.StatusNoContent {
		w.WriteHeader(status)
		return
	}
	s.writeJSON(w, status, result)
}
