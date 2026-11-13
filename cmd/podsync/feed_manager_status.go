package main

// Operational status: download errors, recent downloads, sync summary and logs.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mxpv/podsync/pkg/builder"
	"github.com/mxpv/podsync/pkg/feed"
	"github.com/mxpv/podsync/pkg/logbuffer"
	"github.com/mxpv/podsync/pkg/model"
	"github.com/mxpv/podsync/services/web"
	"github.com/pkg/errors"
	"github.com/robfig/cron/v3"
	log "github.com/sirupsen/logrus"
)

func (m *AppFeedManager) GetDownloadErrors(ctx context.Context, targetFeedID string, limit int) ([]web.DownloadErrorItem, error) {
	if limit <= 0 {
		limit = 20
	} else if limit > 1000 {
		limit = 1000
	}

	m.mu.RLock()
	feeds := make([]*feed.Config, 0, len(m.feeds))
	if targetFeedID != "" {
		if f, ok := m.feeds[targetFeedID]; ok {
			feeds = append(feeds, f)
		}
	} else {
		for _, f := range m.feeds {
			feeds = append(feeds, f)
		}
	}
	errorsMap := make(map[string]feedErrorEntry, len(m.feedErrors))
	for k, v := range m.feedErrors {
		errorsMap[k] = v
	}
	m.mu.RUnlock()

	var items []web.DownloadErrorItem

	collectFromFeed := func(feedConfig *feed.Config) {
		feedTitle := feedConfig.ID
		provider := ""
		if dbFeed, err := m.db.GetFeedMetadata(ctx, feedConfig.ID); err == nil && dbFeed != nil {
			if dbFeed.Title != "" {
				feedTitle = dbFeed.Title
			}
			provider = string(dbFeed.Provider)
		}
		if provider == "" {
			if info, err := builder.ParseURL(feedConfig.URL); err == nil {
				provider = string(info.Provider)
			}
		}

		_ = m.db.WalkEpisodes(ctx, feedConfig.ID, func(ep *model.Episode) error {
			if ep.Status == model.EpisodeError {
				rec := ""
				errStr := ep.Error
				if strings.Contains(errStr, "bot") || strings.Contains(errStr, "cookies") || strings.Contains(errStr, "Sign in") {
					rec = "Configure Netscape cookies.txt file in Web Admin or via PODSYNC_DOWNLOADER_COOKIES_FILE to bypass bot detection"
				} else if strings.Contains(errStr, "429") || strings.Contains(errStr, "Too Many Requests") {
					rec = "Rate limited by source platform; use a proxy or reduce update frequency"
				} else if strings.Contains(errStr, "Private") {
					rec = "Video is private; authentication cookies for the video owner are required"
				} else if strings.Contains(errStr, "timed out") {
					rec = "Download timed out; check network bandwidth or increase downloader timeout"
				}

				item := web.DownloadErrorItem{
					FeedID:         feedConfig.ID,
					FeedTitle:      feedTitle,
					FeedProvider:   provider,
					EpisodeID:      ep.ID,
					EpisodeTitle:   ep.Title,
					VideoURL:       ep.VideoURL,
					PubDate:        ep.PubDate,
					Error:          errStr,
					ErrorAt:        ep.ErrorAt,
					Recommendation: rec,
				}
				if item.Error == "" {
					item.Error = "Download failed (error exit status)"
				}
				items = append(items, item)
			}
			return nil
		})

		if feedErr, hasErr := errorsMap[feedConfig.ID]; hasErr {
			rec := "Check logs or API configuration"
			errLower := strings.ToLower(feedErr.Error)
			if strings.Contains(errLower, "token is required") || strings.Contains(errLower, "not loaded") || strings.Contains(errLower, "api token") {
				rec = "Add the required API token for " + provider + " in the API Tokens tab"
			}
			errItem := web.DownloadErrorItem{
				FeedID:         feedConfig.ID,
				FeedTitle:      feedTitle,
				FeedProvider:   provider,
				EpisodeID:      "feed_update",
				EpisodeTitle:   "Feed Update Failure",
				VideoURL:       feedConfig.URL,
				Error:          feedErr.Error,
				ErrorAt:        &feedErr.Time,
				Recommendation: rec,
			}
			items = append(items, errItem)
		}
	}

	for _, f := range feeds {
		collectFromFeed(f)
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].ErrorAt != nil && items[j].ErrorAt != nil {
			return items[i].ErrorAt.After(*items[j].ErrorAt)
		}
		return items[i].PubDate.After(items[j].PubDate)
	})

	if len(items) > limit {
		items = items[:limit]
	}

	return items, nil
}

func (m *AppFeedManager) GetRecentDownloads(ctx context.Context, targetFeedID string, limit int) ([]web.RecentDownloadItem, error) {
	if limit <= 0 {
		limit = 10
	} else if limit > 100 {
		limit = 100
	}

	m.mu.RLock()
	feeds := make([]*feed.Config, 0, len(m.feeds))
	if targetFeedID != "" {
		if f, ok := m.feeds[targetFeedID]; ok {
			feeds = append(feeds, f)
		}
	} else {
		for _, f := range m.feeds {
			feeds = append(feeds, f)
		}
	}

	host := "http://localhost:8080"
	mountPath := ""
	if m.cfg != nil {
		if m.cfg.Server.Hostname != "" {
			host = m.cfg.Server.Hostname
		} else if m.cfg.Server.Port != 0 {
			host = fmt.Sprintf("http://localhost:%d", m.cfg.Server.Port)
		}
		if m.cfg.Server.Path != "" {
			mountPath = "/" + strings.Trim(m.cfg.Server.Path, "/")
		}
	}
	m.mu.RUnlock()

	host = strings.TrimSuffix(host, "/")

	var items []web.RecentDownloadItem

	collectFromFeed := func(feedConfig *feed.Config) {
		feedTitle := feedConfig.ID
		provider := ""
		if dbFeed, err := m.db.GetFeedMetadata(ctx, feedConfig.ID); err == nil && dbFeed != nil {
			if dbFeed.Title != "" {
				feedTitle = dbFeed.Title
			}
			provider = string(dbFeed.Provider)
		}
		if provider == "" {
			if info, err := builder.ParseURL(feedConfig.URL); err == nil {
				provider = string(info.Provider)
			}
		}

		_ = m.db.WalkEpisodes(ctx, feedConfig.ID, func(ep *model.Episode) error {
			if ep.Status == model.EpisodeDownloaded {
				epName := feed.EpisodeName(feedConfig, ep)
				mediaURL := fmt.Sprintf("%s%s/%s/%s", host, mountPath, feedConfig.ID, epName)
				items = append(items, web.RecentDownloadItem{
					FeedID:       feedConfig.ID,
					FeedTitle:    feedTitle,
					FeedProvider: provider,
					EpisodeID:    ep.ID,
					EpisodeTitle: ep.Title,
					VideoURL:     ep.VideoURL,
					PubDate:      ep.PubDate,
					Size:         ep.Size,
					Duration:     ep.Duration,
					MediaURL:     mediaURL,
				})
			}
			return nil
		})
	}

	for _, f := range feeds {
		collectFromFeed(f)
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].PubDate.After(items[j].PubDate)
	})

	if len(items) > limit {
		items = items[:limit]
	}

	return items, nil
}

func (m *AppFeedManager) GetSyncStatus(ctx context.Context) (*web.SyncStatusInfo, error) {
	m.mu.RLock()
	feeds := make([]*feed.Config, 0, len(m.feeds))
	for _, f := range m.feeds {
		feeds = append(feeds, f)
	}
	hasDl := m.downloader != nil
	cronEntries := make(map[string]cron.EntryID, len(m.cronEntries))
	for k, v := range m.cronEntries {
		cronEntries[k] = v
	}
	c := m.cron
	m.mu.RUnlock()

	info := &web.SyncStatusInfo{
		ActiveFeeds: len(feeds),
		Feeds:       make([]web.SyncStatusFeedInfo, 0, len(feeds)),
	}

	// Binary presence
	info.HasDownloader = hasDl
	if _, err := exec.LookPath("ffmpeg"); err == nil {
		info.HasFFmpeg = true
	}

	// Disk stats
	if stats, err := m.GetSystemStats(ctx); err == nil && stats != nil {
		info.DiskFreeBytes = stats.DiskFree
		info.DiskTotalBytes = stats.DiskTotal
	}

	past24h := time.Now().Add(-24 * time.Hour)

	for _, f := range feeds {
		feedTitle := f.ID
		provider := ""
		var lastUpdate *time.Time
		if dbFeed, err := m.db.GetFeedMetadata(ctx, f.ID); err == nil && dbFeed != nil {
			if dbFeed.Title != "" {
				feedTitle = dbFeed.Title
			}
			provider = string(dbFeed.Provider)
			if !dbFeed.UpdatedAt.IsZero() {
				lastUpdate = &dbFeed.UpdatedAt
			}
		}
		if provider == "" {
			if pInfo, err := builder.ParseURL(f.URL); err == nil {
				provider = string(pInfo.Provider)
			}
		}

		var nextUpdate *time.Time
		if c != nil {
			if entryID, exists := cronEntries[f.ID]; exists {
				nextTime := c.Entry(entryID).Next
				if !nextTime.IsZero() {
					nextUpdate = &nextTime
				}
			}
		}

		var epCount, dlCount, errCount int
		_ = m.db.WalkEpisodes(ctx, f.ID, func(ep *model.Episode) error {
			epCount++
			switch ep.Status {
			case model.EpisodeDownloaded:
				dlCount++
				if ep.PubDate.After(past24h) {
					info.DownloadedPast24h++
				}
			case model.EpisodeError:
				errCount++
			}
			return nil
		})

		info.TotalEpisodes += epCount
		info.DownloadedEpisodes += dlCount
		info.FailedEpisodes += errCount

		info.Feeds = append(info.Feeds, web.SyncStatusFeedInfo{
			FeedID:          f.ID,
			FeedTitle:       feedTitle,
			URL:             f.URL,
			Provider:        provider,
			CronSchedule:    f.CronSchedule,
			LastUpdate:      lastUpdate,
			NextUpdate:      nextUpdate,
			EpisodeCount:    epCount,
			DownloadedCount: dlCount,
			ErrorCount:      errCount,
		})
	}

	sort.Slice(info.Feeds, func(i, j int) bool {
		return info.Feeds[i].FeedID < info.Feeds[j].FeedID
	})

	return info, nil
}

func (m *AppFeedManager) RecordFeedUpdateError(feedID string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.feedErrors == nil {
		m.feedErrors = make(map[string]feedErrorEntry)
	}
	if err != nil {
		m.feedErrors[feedID] = feedErrorEntry{
			Error: err.Error(),
			Time:  time.Now(),
		}
	} else {
		delete(m.feedErrors, feedID)
	}
}

func (m *AppFeedManager) ClearFeedUpdateError(feedID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.feedErrors != nil {
		delete(m.feedErrors, feedID)
	}
}

func (m *AppFeedManager) GetSystemLogs(ctx context.Context, filter web.LogFilter) (*web.LogResult, error) {
	if m.logBuffer == nil {
		return &web.LogResult{
			CurrentLevel: log.GetLevel().String(),
		}, nil
	}

	res := m.logBuffer.Query(logbuffer.Filter{
		Level:   filter.Level,
		Search:  filter.Search,
		FeedID:  filter.FeedID,
		Limit:   filter.Limit,
		SinceID: filter.SinceID,
	})

	entries := make([]web.LogEntry, len(res.Entries))
	for i, e := range res.Entries {
		entries[i] = web.LogEntry{
			ID:        e.ID,
			Timestamp: e.Timestamp,
			Level:     e.Level,
			Message:   e.Message,
			Fields:    e.Fields,
			Caller:    e.Caller,
		}
	}

	return &web.LogResult{
		Entries:       entries,
		Total:         res.Total,
		FilteredTotal: res.FilteredTotal,
		MaxID:         res.MaxID,
		CurrentLevel:  res.CurrentLevel,
	}, nil
}

func (m *AppFeedManager) ClearSystemLogs(ctx context.Context) error {
	if m.logBuffer != nil {
		m.logBuffer.Clear()
	}
	return nil
}

func (m *AppFeedManager) SetLogLevel(ctx context.Context, levelStr string) (string, error) {
	lvl, err := log.ParseLevel(levelStr)
	if err != nil {
		return "", errors.Wrapf(err, "invalid log level %q", levelStr)
	}
	log.SetLevel(lvl)
	log.Infof("system log level dynamically set to %s", lvl.String())
	return lvl.String(), nil
}

func (m *AppFeedManager) GetLogLevel(ctx context.Context) string {
	return log.GetLevel().String()
}

func (m *AppFeedManager) DownloadLogs(ctx context.Context) ([]byte, string, error) {
	if m.cfg != nil && m.cfg.Log.Filename != "" {
		if data, err := os.ReadFile(m.cfg.Log.Filename); err == nil && len(data) > 0 {
			filename := filepath.Base(m.cfg.Log.Filename)
			return data, filename, nil
		}
	}

	if m.logBuffer != nil {
		content := m.logBuffer.FormatPlainText(logbuffer.Filter{Limit: 2000})
		return []byte(content), "podsync.log", nil
	}

	return []byte("No logs available\n"), "podsync.log", nil
}

func (m *AppFeedManager) GetAudiobooksDir() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.audiobooksDirLocked()
}
