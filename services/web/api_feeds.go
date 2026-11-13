// Feed, token, cover and preview REST handlers.
package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/mxpv/podsync/pkg/builder"
	"github.com/mxpv/podsync/pkg/feed"
	"github.com/mxpv/podsync/pkg/model"
)

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
				s.writeJSON(w, http.StatusOK, page)
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

	if info.Provider == model.ProviderRSS || info.Provider == model.ProviderApplePodcasts {
		var (
			meta     *builder.ParsedRSSMetadata
			fetchErr error
		)
		if info.Provider == model.ProviderApplePodcasts {
			appleBuilder := builder.NewApplePodcastsBuilderWithHTTPClient(s.getHTTPClient())
			meta, fetchErr = appleBuilder.FetchFeed(r.Context(), rawURL)
		} else {
			rssBuilder := builder.NewRSSBuilderWithHTTPClient(s.getHTTPClient())
			meta, fetchErr = rssBuilder.FetchFeed(r.Context(), rawURL)
		}
		if fetchErr != nil {
			s.writeError(w, http.StatusBadRequest, "failed to fetch podcast feed: "+fetchErr.Error())
			return
		}

		s.writeJSON(w, http.StatusOK, FeedPreviewResponse{
			Provider:      string(info.Provider),
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
