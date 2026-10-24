package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pelletier/go-toml"
	"github.com/pkg/errors"
	"github.com/robfig/cron/v3"
	log "github.com/sirupsen/logrus"

	"github.com/mxpv/podsync/pkg/builder"
	"github.com/mxpv/podsync/pkg/db"
	"github.com/mxpv/podsync/pkg/feed"
	"github.com/mxpv/podsync/pkg/fs"
	"github.com/mxpv/podsync/pkg/jobqueue"
	"github.com/mxpv/podsync/pkg/logbuffer"
	"github.com/mxpv/podsync/pkg/model"
	"github.com/mxpv/podsync/pkg/ytdl"
	"github.com/mxpv/podsync/services/update"
	"github.com/mxpv/podsync/services/web"
)

type feedErrorEntry struct {
	Error string
	Time  time.Time
}

type feedStats struct {
	episodeCount    int
	downloadedCount int
	errorCount      int
	totalSize       int64
	updatedAt       time.Time
}

type AppFeedManager struct {
	mu             sync.RWMutex
	configPath     string
	cfg            *Config
	feeds          map[string]*feed.Config
	tokens         map[model.Provider]StringSlice
	downloader     *ytdl.YoutubeDl
	cron           *cron.Cron
	cronEntries    map[string]cron.EntryID
	updates        chan *feed.Config
	updater        *update.Manager
	db             db.Storage
	storage        fs.Storage
	startedAt      time.Time
	version        string
	commit         string
	buildDate      string
	logBuffer      *logbuffer.Buffer
	feedErrors     map[string]feedErrorEntry
	queuedUpdates  map[string]int
	runningUpdates map[string]bool
	coverCache     sync.Map
	statsMu        sync.RWMutex
	statsCache     map[string]feedStats
	queue          *jobqueue.JobQueue
}

func NewAppFeedManager(
	configPath string,
	cfg *Config,
	feeds map[string]*feed.Config,
	tokens map[model.Provider]StringSlice,
	downloader *ytdl.YoutubeDl,
	c *cron.Cron,
	cronEntries map[string]cron.EntryID,
	updates chan *feed.Config,
	updater *update.Manager,
	database db.Storage,
	storage fs.Storage,
	version, commit, buildDate string,
	logBuffer *logbuffer.Buffer,
) *AppFeedManager {
	var q *jobqueue.JobQueue
	if database != nil {
		maxAttempts := 5
		if cfg != nil && cfg.Jobs.MaxAttempts > 0 {
			maxAttempts = cfg.Jobs.MaxAttempts
		}
		q = jobqueue.NewJobQueue(database, maxAttempts)
		if cfg != nil && cfg.Jobs.PollPeriod != "" {
			if d, err := time.ParseDuration(cfg.Jobs.PollPeriod); err == nil && d > 0 {
				q.SetPollPeriod(d)
			}
		}
	}
	mgr := &AppFeedManager{
		queue:          q,
		configPath:     configPath,
		cfg:            cfg,
		feeds:          feeds,
		tokens:         tokens,
		downloader:     downloader,
		cron:           c,
		cronEntries:    cronEntries,
		updates:        updates,
		updater:        updater,
		db:             database,
		storage:        storage,
		startedAt:      time.Now(),
		version:        version,
		commit:         commit,
		buildDate:      buildDate,
		logBuffer:      logBuffer,
		feedErrors:     make(map[string]feedErrorEntry),
		queuedUpdates:  make(map[string]int),
		runningUpdates: make(map[string]bool),
		statsCache:     make(map[string]feedStats),
	}
	mgr.MountAllAudiobooks()
	return mgr
}

func (m *AppFeedManager) mountAudiobookFeed(f *feed.Config) {
	mounter, ok := m.storage.(fs.Mounter)
	if !ok {
		return
	}
	info, err := builder.ParseURL(f.URL)
	if err != nil || info.Provider != model.ProviderAudiobook {
		return
	}
	baseDir := ""
	if m.cfg != nil && m.cfg.Audiobooks.Dir != "" {
		baseDir = m.cfg.Audiobooks.Dir
	}
	resolvedDir, err := builder.ResolveBookDirectory(baseDir, f, info.ItemID)
	if err != nil {
		log.WithError(err).Warnf("failed to resolve directory for audiobook feed %q", f.ID)
		return
	}
	mounter.Mount(f.ID, resolvedDir)
	log.Infof("mounted audiobook feed %q -> %s", f.ID, resolvedDir)
}

func (m *AppFeedManager) MountAllAudiobooks() {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, f := range m.feeds {
		m.mountAudiobookFeed(f)
	}
}

func (m *AppFeedManager) ListFeeds(ctx context.Context) ([]web.FeedSummary, error) {
	m.mu.RLock()
	feedList := make([]*feed.Config, 0, len(m.feeds))
	for _, f := range m.feeds {
		feedList = append(feedList, f)
	}
	m.mu.RUnlock()

	result := make([]web.FeedSummary, 0, len(feedList))
	for _, f := range feedList {
		summary := m.buildSummary(ctx, f)
		result = append(result, summary)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})

	return result, nil
}

func (m *AppFeedManager) GetFeedsStatus(ctx context.Context) ([]web.FeedStatusItem, error) {
	var activeJobs map[string]*model.UpdateJob
	if m.queue != nil {
		activeJobs, _ = m.queue.GetActiveJobsMap(ctx)
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]web.FeedStatusItem, 0, len(m.feeds))
	for id := range m.feeds {
		status := web.FeedStatusIdle
		var lastErr string
		var attempts int
		var nextRetry *time.Time

		if m.runningUpdates[id] {
			status = web.FeedStatusRunning
		} else if m.queuedUpdates[id] > 0 {
			status = web.FeedStatusQueued
		}

		if activeJobs != nil {
			if job := activeJobs[id]; job != nil {
				switch job.Status {
				case model.JobStatusRunning:
					status = web.FeedStatusRunning
				case model.JobStatusQueued:
					status = web.FeedStatusQueued
				case model.JobStatusRetrying:
					status = web.FeedStatusRetrying
					lastErr = job.LastError
					attempts = job.Attempts
					t := job.AvailableAt
					nextRetry = &t
				}
			}
		}

		result = append(result, web.FeedStatusItem{
			ID:          id,
			Status:      status,
			LastError:   lastErr,
			Attempts:    attempts,
			NextRetryAt: nextRetry,
		})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})

	return result, nil
}

func (m *AppFeedManager) GetFeedDetail(ctx context.Context, id string) (*web.FeedSummary, []*model.Episode, error) {
	m.mu.RLock()
	f, ok := m.feeds[id]
	audiobooksDir := ""
	if m.cfg != nil {
		audiobooksDir = m.cfg.Audiobooks.Dir
	}
	m.mu.RUnlock()

	if !ok {
		return nil, nil, errors.Errorf("feed %q not found", id)
	}

	summary := m.buildSummary(ctx, f)

	episodes := make([]*model.Episode, 0)
	if err := m.db.WalkEpisodes(ctx, id, func(episode *model.Episode) error {
		copy := *episode
		episodes = append(episodes, &copy)
		return nil
	}); err != nil {
		return nil, nil, errors.Wrap(err, "failed to load episodes")
	}

	info, parseErr := builder.ParseURL(f.URL)
	isAudiobook := (parseErr == nil && info.Provider == model.ProviderAudiobook) || strings.HasPrefix(f.URL, "audiobook://")

	// If database has no episodes yet (e.g. prior to cron update), dynamically scan local audiobook directory
	if len(episodes) == 0 && isAudiobook {
		baseDir := audiobooksDir
		abBuilder, bErr := builder.NewAudiobookBuilder(baseDir)
		if bErr == nil {
			if builtFeed, bErr := abBuilder.Build(ctx, f); bErr == nil && builtFeed != nil {
				episodes = builtFeed.Episodes
				summary.EpisodeCount = len(episodes)
				summary.DownloadedCount = len(episodes)
			}
		}
	}

	if isAudiobook || f.PlaylistSort == model.SortingAsc {
		sort.Slice(episodes, func(i, j int) bool {
			if episodes[i].PubDate.Equal(episodes[j].PubDate) {
				return builder.NaturalLess(episodes[i].ID, episodes[j].ID)
			}
			return episodes[i].PubDate.Before(episodes[j].PubDate)
		})
	} else {
		sort.Slice(episodes, func(i, j int) bool {
			if episodes[i].PubDate.Equal(episodes[j].PubDate) {
				return builder.NaturalLess(episodes[i].ID, episodes[j].ID)
			}
			return episodes[i].PubDate.After(episodes[j].PubDate)
		})
	}

	for _, ep := range episodes {
		if ep.Status != model.EpisodeDownloaded {
			continue
		}
		filename := feed.EpisodeName(f, ep)
		if isAudiobook {
			filename = ep.ID
		} else if m.storage != nil {
			if _, err := m.storage.Size(ctx, id+"/"+filename); err != nil {
				legacy := feed.LegacyEpisodeName(f, ep)
				if _, err := m.storage.Size(ctx, id+"/"+legacy); err == nil {
					filename = legacy
				}
			}
		}
		ep.MediaURL = strings.TrimSuffix(summary.FeedURL, id+".xml") + url.PathEscape(id) + "/" + url.PathEscape(filename)
	}
	return &summary, episodes, nil
}

func (m *AppFeedManager) GetFeedEpisodes(ctx context.Context, id string, query web.EpisodeQuery) (*web.EpisodePageResult, error) {
	m.mu.RLock()
	f, ok := m.feeds[id]
	audiobooksDir := ""
	if m.cfg != nil {
		audiobooksDir = m.cfg.Audiobooks.Dir
	}
	m.mu.RUnlock()

	if !ok {
		return nil, errors.Errorf("feed %q not found", id)
	}

	info, parseErr := builder.ParseURL(f.URL)
	isAudiobook := (parseErr == nil && info.Provider == model.ProviderAudiobook) || strings.HasPrefix(f.URL, "audiobook://")

	var allEpisodes []*model.Episode
	totalCount := 0

	if err := m.db.WalkEpisodes(ctx, id, func(episode *model.Episode) error {
		totalCount++
		copy := *episode
		allEpisodes = append(allEpisodes, &copy)
		return nil
	}); err != nil {
		return nil, errors.Wrap(err, "failed to load episodes")
	}

	if len(allEpisodes) == 0 && isAudiobook {
		baseDir := audiobooksDir
		if abBuilder, bErr := builder.NewAudiobookBuilder(baseDir); bErr == nil {
			if builtFeed, bErr := abBuilder.Build(ctx, f); bErr == nil && builtFeed != nil {
				allEpisodes = builtFeed.Episodes
				totalCount = len(allEpisodes)
			}
		}
	}

	search := strings.TrimSpace(strings.ToLower(query.Search))
	status := strings.TrimSpace(strings.ToLower(query.Status))
	duration := strings.TrimSpace(strings.ToLower(query.Duration))

	var filtered []*model.Episode
	for _, ep := range allEpisodes {
		if status != "" && status != "all" && string(ep.Status) != status {
			continue
		}
		if search != "" {
			combined := strings.ToLower(ep.Title + " " + ep.ID)
			if !strings.Contains(combined, search) {
				continue
			}
		}
		if duration != "" && duration != "all" {
			dur := ep.Duration
			if duration == "short" && (dur <= 0 || dur >= 900) {
				continue
			}
			if duration == "medium" && (dur < 900 || dur > 3600) {
				continue
			}
			if duration == "long" && dur <= 3600 {
				continue
			}
		}
		filtered = append(filtered, ep)
	}

	matchingCount := len(filtered)

	sortOrder := query.Sort
	if sortOrder == "" || sortOrder == "default" {
		if isAudiobook || f.PlaylistSort == model.SortingAsc {
			sortOrder = "date-asc"
		} else {
			sortOrder = "date-desc"
		}
	}

	switch sortOrder {
	case "date-asc":
		sort.Slice(filtered, func(i, j int) bool {
			if filtered[i].PubDate.Equal(filtered[j].PubDate) {
				return builder.NaturalLess(filtered[i].ID, filtered[j].ID)
			}
			return filtered[i].PubDate.Before(filtered[j].PubDate)
		})
	case "date-desc":
		sort.Slice(filtered, func(i, j int) bool {
			if filtered[i].PubDate.Equal(filtered[j].PubDate) {
				return builder.NaturalLess(filtered[i].ID, filtered[j].ID)
			}
			return filtered[i].PubDate.After(filtered[j].PubDate)
		})
	case "title-asc":
		sort.Slice(filtered, func(i, j int) bool {
			ti := filtered[i].Title
			if ti == "" {
				ti = filtered[i].ID
			}
			tj := filtered[j].Title
			if tj == "" {
				tj = filtered[j].ID
			}
			return strings.ToLower(ti) < strings.ToLower(tj)
		})
	case "duration-desc":
		sort.Slice(filtered, func(i, j int) bool {
			return filtered[i].Duration > filtered[j].Duration
		})
	case "duration-asc":
		sort.Slice(filtered, func(i, j int) bool {
			return filtered[i].Duration < filtered[j].Duration
		})
	}

	limit := query.Limit
	if limit <= 0 {
		limit = 40
	} else if limit > 200 {
		limit = 200
	}

	offset := query.Offset
	if offset < 0 {
		offset = 0
	}
	if offset > matchingCount {
		offset = matchingCount
	}

	end := offset + limit
	if end > matchingCount {
		end = matchingCount
	}

	paged := filtered[offset:end]
	hasMore := end < matchingCount
	nextOffset := 0
	if hasMore {
		nextOffset = end
	}

	summary := m.buildSummary(ctx, f)
	mediaBase := strings.TrimSuffix(summary.FeedURL, id+".xml") + url.PathEscape(id) + "/"

	for _, ep := range paged {
		if ep.Status != model.EpisodeDownloaded {
			continue
		}
		filename := feed.EpisodeName(f, ep)
		if isAudiobook {
			filename = ep.ID
		} else if m.storage != nil {
			if _, err := m.storage.Size(ctx, id+"/"+filename); err != nil {
				legacy := feed.LegacyEpisodeName(f, ep)
				if _, err := m.storage.Size(ctx, id+"/"+legacy); err == nil {
					filename = legacy
				}
			}
		}
		ep.MediaURL = mediaBase + url.PathEscape(filename)
	}

	return &web.EpisodePageResult{
		Episodes:   paged,
		Total:      totalCount,
		Matching:   matchingCount,
		Offset:     offset,
		Limit:      limit,
		HasMore:    hasMore,
		NextOffset: nextOffset,
	}, nil
}

// extractCoverArtFromXML reads the generated <feed_id>.xml from storage and extracts <image><url>...</url></image>
func (m *AppFeedManager) extractCoverArtFromXML(feedID string) string {
	if val, ok := m.coverCache.Load(feedID); ok {
		if s, ok := val.(string); ok && s != "" {
			return s
		}
	}

	if m.storage == nil {
		return ""
	}
	xmlName := fmt.Sprintf("%s.xml", feedID)
	file, err := m.storage.Open(xmlName)
	if err != nil || file == nil {
		return ""
	}
	defer file.Close()

	buf := make([]byte, 32768)
	n, _ := file.Read(buf)
	if n <= 0 {
		return ""
	}

	xmlContent := string(buf[:n])
	var cover string
	// Match <image> ... <url>URL</url>
	re := regexp.MustCompile(`(?s)<image>.*?<url>\s*([^<\s]+)\s*</url>`)
	if match := re.FindStringSubmatch(xmlContent); len(match) > 1 {
		cover = strings.TrimSpace(match[1])
	} else {
		// Fallback to <itunes:image href="URL"
		itunesRe := regexp.MustCompile(`<itunes:image\s+href="([^"]+)"`)
		if match := itunesRe.FindStringSubmatch(xmlContent); len(match) > 1 {
			cover = strings.TrimSpace(match[1])
		}
	}

	if cover != "" {
		m.coverCache.Store(feedID, cover)
	}
	return cover
}

func (m *AppFeedManager) buildSummary(ctx context.Context, f *feed.Config) web.FeedSummary {
	m.mu.RLock()
	updating := m.queuedUpdates[f.ID] > 0 || m.runningUpdates[f.ID]
	errEntry, hasErr := m.feedErrors[f.ID]
	host := web.Config{}.PublicURL()
	if m.cfg != nil {
		host = m.cfg.Server.PublicURL()
	}
	m.mu.RUnlock()

	summary := web.FeedSummary{
		ID:           f.ID,
		URL:          f.URL,
		Format:       string(f.Format),
		Quality:      string(f.Quality),
		PageSize:     f.PageSize,
		CronSchedule: f.CronSchedule,
		UpdatePeriod: f.UpdatePeriod.String(),
		PrivateFeed:  f.PrivateFeed,
		Updating:     updating,
		Config:       f,
	}

	// Try to get enriched metadata from DB
	if dbFeed, err := m.db.GetFeedMetadata(ctx, f.ID); err == nil && dbFeed != nil {
		summary.Title = dbFeed.Title
		summary.Description = dbFeed.Description
		summary.Author = dbFeed.Author
		summary.CoverArt = dbFeed.CoverArt
		summary.Provider = string(dbFeed.Provider)
		if !dbFeed.UpdatedAt.IsZero() {
			summary.LastUpdate = &dbFeed.UpdatedAt
		}
		if dbFeed.PrivateFeed {
			summary.PrivateFeed = true
		}
	}

	if f.Custom.Author != "" {
		summary.Author = f.Custom.Author
	} else {
		cleanAuthor := strings.TrimSpace(strings.ToLower(summary.Author))
		if cleanAuthor == "<notfound>" || cleanAuthor == "notfound" || strings.Contains(cleanAuthor, "notfound") {
			if summary.Title != "" {
				summary.Author = summary.Title
			} else {
				summary.Author = ""
			}
		}
	}

	// Always extract/override cover art from the generated XML (<image><url>...</url></image>)
	if xmlCover := m.extractCoverArtFromXML(f.ID); xmlCover != "" {
		summary.CoverArt = xmlCover
	}

	if summary.Provider == "" {
		if info, err := builder.ParseURL(f.URL); err == nil {
			summary.Provider = string(info.Provider)
		}
	}

	// Count episodes, errors, total size via cached statistics
	epCount, dlCount, errCount, totalSize := m.getFeedStats(ctx, f)
	summary.EpisodeCount = epCount
	summary.DownloadedCount = dlCount
	summary.ErrorCount = errCount
	summary.TotalSize = totalSize

	if hasErr {
		summary.LastError = errEntry.Error
		summary.LastErrorAt = &errEntry.Time
	}

	// Generate URLs using the same base as RSS enclosure links.
	feedURL := fmt.Sprintf("%s/%s.xml", host, f.ID)
	summary.FeedURL = feedURL

	cleanURL := strings.TrimPrefix(feedURL, "https://")
	cleanURL = strings.TrimPrefix(cleanURL, "http://")

	summary.AppleURL = fmt.Sprintf("podcast://%s", cleanURL)
	summary.PocketCastsURL = fmt.Sprintf("pktc://subscribe/%s", cleanURL)
	summary.OvercastURL = fmt.Sprintf("overcast://x-callback-url/add?url=%s", url.QueryEscape(feedURL))

	return summary
}

// Audiobooks are refreshed on import, settings changes, and manual updates.
func isAudiobookFeed(f *feed.Config) bool {
	info, err := builder.ParseURL(f.URL)
	return err == nil && info.Provider == model.ProviderAudiobook
}

func (m *AppFeedManager) AddFeed(ctx context.Context, newFeed *feed.Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(newFeed.ID) {
		return errors.New("feed ID must contain only Latin letters, digits, underscores, or hyphens")
	}

	if newFeed.ID == "personal" || strings.HasPrefix(newFeed.ID, "personal-") || newFeed.ID == "_personal" {
		return errors.New("this feed ID is reserved for personal feeds")
	}
	if _, exists := m.feeds[newFeed.ID]; exists {
		return errors.Errorf("feed with ID %q already exists", newFeed.ID)
	}

	if isAudiobookFeed(newFeed) {
		newFeed.CronSchedule = ""
	} else if newFeed.CronSchedule == "" {
		newFeed.CronSchedule = "@every 12h"
	}

	if !isAudiobookFeed(newFeed) {
		if _, err := cron.ParseStandard(newFeed.CronSchedule); err != nil {
			return errors.Wrap(err, "invalid update schedule")
		}
	}
	if err := m.checkUpdateCapacityLocked(); err != nil {
		return err
	}

	if err := m.saveFeedToConfig(newFeed); err != nil {
		return errors.Wrap(err, "failed to save feed configuration")
	}

	// Add to in-memory map
	m.feeds[newFeed.ID] = newFeed
	if m.updater != nil {
		m.updater.SetFeedConfig(newFeed.ID, newFeed)
	}

	// Mount if audiobook
	m.mountAudiobookFeed(newFeed)

	// Add remote feeds to cron.
	if m.cron != nil && !isAudiobookFeed(newFeed) {
		feedID := newFeed.ID
		cronID, err := m.cron.AddFunc(newFeed.CronSchedule, func() {
			m.QueueScheduledUpdate(feedID)
		})
		if err == nil {
			m.cronEntries[newFeed.ID] = cronID
		} else {
			log.WithError(err).Warnf("failed to register cron for feed %q", newFeed.ID)
		}
	}

	return m.queueUpdateLocked(newFeed)
}

func (m *AppFeedManager) DeleteFeed(ctx context.Context, id string, deleteFiles bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	f, exists := m.feeds[id]
	if !exists {
		return errors.Errorf("feed %q not found", id)
	}
	if err := m.deleteFeedFromConfig(id); err != nil {
		return errors.Wrapf(err, "failed to remove feed %q from configuration", id)
	}

	// Unmount if mounted
	if mounter, ok := m.storage.(fs.Mounter); ok {
		mounter.Unmount(id)
	}

	// Remove from cron
	if cronID, ok := m.cronEntries[id]; ok {
		m.cron.Remove(cronID)
		delete(m.cronEntries, id)
	}

	// Remove from map
	delete(m.feeds, id)
	if m.updater != nil {
		m.updater.SetFeedConfig(id, nil)
	}
	m.coverCache.Delete(id)

	// Clean database
	if err := m.db.DeleteFeed(ctx, id); err != nil {
		log.WithError(err).Warnf("failed to delete feed %q from database", id)
	}

	// Optionally delete files from disk/storage
	if deleteFiles {
		_ = m.storage.Delete(ctx, fmt.Sprintf("%s.xml", id))
		info, err := builder.ParseURL(f.URL)
		isAudiobook := (err == nil && info.Provider == model.ProviderAudiobook)
		if m.cfg.Storage.Type == "local" && !isAudiobook {
			feedDir := filepath.Join(m.cfg.Storage.Local.DataDir, id)
			_ = os.RemoveAll(feedDir)
			log.Infof("deleted files for feed %q at %s", id, feedDir)
		}
	}

	_ = f
	return nil
}

func (m *AppFeedManager) TriggerUpdate(ctx context.Context, id string) error {
	m.mu.RLock()
	f, ok := m.feeds[id]
	m.mu.RUnlock()
	if !ok {
		return errors.Errorf("feed %q not found", id)
	}

	if m.queue != nil {
		job, created, err := m.queue.EnqueueManualUpdate(ctx, id)
		if err != nil {
			return err
		}
		if !created {
			log.Infof("feed %q update already in progress (job: %s, status: %s)", id, job.ID, job.Status)
			return nil
		}
		m.mu.Lock()
		m.markQueuedLocked(id)
		m.mu.Unlock()

		log.Infof("manually triggered update for feed %q (job: %s)", id, job.ID)
		return nil
	}

	// Legacy channel fallback (m.queue == nil)
	m.mu.Lock()
	m.markQueuedLocked(id)
	m.mu.Unlock()

	select {
	case m.updates <- f:
		log.Infof("manually triggered update for feed %q", id)
		return nil
	default:
		m.mu.Lock()
		if m.queuedUpdates[id] > 0 {
			m.queuedUpdates[id]--
		}
		m.mu.Unlock()
		return errors.New("update queue is currently full, please try again shortly")
	}
}

func (m *AppFeedManager) TriggerUpdateAll(ctx context.Context) ([]string, error) {
	m.mu.RLock()
	feedList := make([]*feed.Config, 0, len(m.feeds))
	for _, f := range m.feeds {
		feedList = append(feedList, f)
	}
	m.mu.RUnlock()

	if len(feedList) == 0 {
		return []string{}, nil
	}

	var queued []string
	for _, f := range feedList {
		if m.queue != nil {
			_, created, err := m.queue.EnqueueManualUpdate(ctx, f.ID)
			if err != nil {
				log.WithField("feed_id", f.ID).WithError(err).Warn("failed to enqueue update in jobqueue")
				continue
			}
			if created {
				m.mu.Lock()
				m.markQueuedLocked(f.ID)
				m.mu.Unlock()
				queued = append(queued, f.ID)
			}
			continue
		}

		m.mu.Lock()
		if m.queuedUpdates[f.ID] > 0 || (m.runningUpdates != nil && m.runningUpdates[f.ID]) {
			m.mu.Unlock()
			continue
		}
		m.markQueuedLocked(f.ID)
		m.mu.Unlock()

		select {
		case m.updates <- f:
			log.Infof("manually triggered update for feed %q", f.ID)
			queued = append(queued, f.ID)
		default:
			m.mu.Lock()
			if m.queuedUpdates[f.ID] > 0 {
				m.queuedUpdates[f.ID]--
			}
			m.mu.Unlock()
			log.Warnf("failed to queue update for feed %q: queue full", f.ID)
		}
	}

	return queued, nil
}

func (m *AppFeedManager) markQueuedLocked(id string) {
	if m.queuedUpdates == nil {
		m.queuedUpdates = make(map[string]int)
	}
	m.queuedUpdates[id]++
}

func (m *AppFeedManager) MarkFeedUpdateQueued(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.markQueuedLocked(id)
}

func (m *AppFeedManager) invalidateStats(id string) {
	m.statsMu.Lock()
	delete(m.statsCache, id)
	m.statsMu.Unlock()
}

func (m *AppFeedManager) getFeedStats(ctx context.Context, f *feed.Config) (int, int, int, int64) {
	m.statsMu.RLock()
	if s, ok := m.statsCache[f.ID]; ok && time.Since(s.updatedAt) < 15*time.Second {
		m.statsMu.RUnlock()
		return s.episodeCount, s.downloadedCount, s.errorCount, s.totalSize
	}
	m.statsMu.RUnlock()

	var epCount, dlCount, errCount int
	var totalSize int64
	if m.db != nil {
		if stats, err := m.db.GetFeedStats(ctx, f.ID); err == nil && stats != nil {
			epCount = stats.EpisodeCount
			dlCount = stats.DownloadedCount
			errCount = stats.ErrorCount
			totalSize = stats.TotalSize
		} else {
			_ = m.db.WalkEpisodes(ctx, f.ID, func(ep *model.Episode) error {
				epCount++
				switch ep.Status {
				case model.EpisodeDownloaded:
					dlCount++
					totalSize += ep.Size
				case model.EpisodeError:
					errCount++
				}
				return nil
			})
		}
	}

	m.statsMu.Lock()
	if m.statsCache == nil {
		m.statsCache = make(map[string]feedStats)
	}
	m.statsCache[f.ID] = feedStats{
		episodeCount:    epCount,
		downloadedCount: dlCount,
		errorCount:      errCount,
		totalSize:       totalSize,
		updatedAt:       time.Now(),
	}
	m.statsMu.Unlock()

	return epCount, dlCount, errCount, totalSize
}

func (m *AppFeedManager) QueueScheduledUpdate(id string) {
	m.mu.RLock()
	current := m.feeds[id]
	m.mu.RUnlock()

	if current == nil {
		return
	}

	if m.queue != nil {
		job, created, err := m.queue.EnqueueScheduledUpdate(context.Background(), id)
		if err != nil {
			log.WithField("feed_id", id).WithError(err).Warn("failed to enqueue scheduled update in jobqueue")
			return
		}
		if !created {
			log.WithField("feed_id", id).Debugf("feed update already active (status: %s), skipping duplicate scheduled update", job.Status)
			return
		}
		m.mu.Lock()
		m.markQueuedLocked(id)
		m.mu.Unlock()
		return
	}

	m.mu.Lock()
	if m.queuedUpdates[id] > 0 || (m.runningUpdates != nil && m.runningUpdates[id]) {
		m.mu.Unlock()
		log.WithField("feed_id", id).Debug("feed update already queued or running, skipping duplicate")
		return
	}
	m.markQueuedLocked(id)
	m.mu.Unlock()

	select {
	case m.updates <- current:
	default:
		m.mu.Lock()
		if m.queuedUpdates[id] > 0 {
			m.queuedUpdates[id]--
		}
		m.mu.Unlock()
		log.WithField("feed_id", id).Warn("updates channel full, dropping queue request")
	}
}

func (m *AppFeedManager) MarkFeedUpdateStarted(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.queuedUpdates[id] > 0 {
		m.queuedUpdates[id]--
	}
	if m.runningUpdates == nil {
		m.runningUpdates = make(map[string]bool)
	}
	m.runningUpdates[id] = true
}

func (m *AppFeedManager) TryStartFeedUpdate(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.runningUpdates != nil && m.runningUpdates[id] {
		return false
	}
	if m.queuedUpdates[id] > 0 {
		m.queuedUpdates[id]--
	}
	if m.runningUpdates == nil {
		m.runningUpdates = make(map[string]bool)
	}
	m.runningUpdates[id] = true
	return true
}

func (m *AppFeedManager) MarkFeedUpdateFinished(id string) {
	m.mu.Lock()
	delete(m.runningUpdates, id)
	m.mu.Unlock()
	m.coverCache.Delete(id)
	m.invalidateStats(id)
}

func (m *AppFeedManager) RetryEpisode(ctx context.Context, feedID, episodeID string) error {
	err := m.db.UpdateEpisode(feedID, episodeID, func(ep *model.Episode) error {
		ep.Status = model.EpisodeNew
		ep.Error = ""
		ep.ErrorAt = nil
		return nil
	})
	if err != nil {
		return err
	}
	m.invalidateStats(feedID)
	return m.TriggerUpdate(ctx, feedID)
}

func (m *AppFeedManager) JobQueue() *jobqueue.JobQueue {
	return m.queue
}

func (m *AppFeedManager) GetFeedConfig(id string) *feed.Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.feeds[id]
}

func (m *AppFeedManager) GetTokens(ctx context.Context) []web.TokenInfo {
	m.mu.RLock()
	tokensCopy := make(map[model.Provider]StringSlice, len(m.tokens))
	for k, v := range m.tokens {
		tokensCopy[k] = append([]string(nil), v...)
	}
	m.mu.RUnlock()

	providers := []struct {
		provider model.Provider
		envVars  []string
	}{
		{model.ProviderVkVideo, []string{"PODSYNC_VKVIDEO_API_KEY", "PODSYNC_VK_API_KEY"}},
		{model.ProviderYoutube, []string{"PODSYNC_YOUTUBE_API_KEY"}},
		{model.ProviderVimeo, []string{"PODSYNC_VIMEO_API_KEY"}},
		{model.ProviderSoundcloud, []string{"PODSYNC_SOUNDCLOUD_API_KEY"}},
		{model.ProviderTwitch, []string{"PODSYNC_TWITCH_API_KEY"}},
		{model.ProviderRutube, []string{"PODSYNC_RUTUBE_API_KEY"}},
	}

	result := make([]web.TokenInfo, 0, len(providers))
	for _, p := range providers {
		fromEnv := false
		for _, v := range p.envVars {
			if os.Getenv(v) != "" {
				fromEnv = true
				break
			}
		}

		var rawTokens []string
		if list, ok := tokensCopy[p.provider]; ok {
			rawTokens = list
		}

		maskedTokens := make([]string, 0, len(rawTokens))
		for _, tok := range rawTokens {
			maskedTokens = append(maskedTokens, maskToken(tok))
		}

		result = append(result, web.TokenInfo{
			Provider: string(p.provider),
			Tokens:   maskedTokens,
			FromEnv:  fromEnv,
		})
	}

	return result
}

func maskToken(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= 8 {
		return "••••••••"
	}
	return s[:4] + "••••" + s[len(s)-4:]
}

func (m *AppFeedManager) persistTokensLocked(providerStr string, newTokens []string) error {
	provider := model.Provider(providerStr)
	// Save to config.toml
	if m.configPath != "" {
		tree, err := toml.LoadFile(m.configPath)
		if err == nil {
			key := fmt.Sprintf("tokens.%s", providerStr)
			if len(newTokens) == 0 {
				tree.Delete(key)
			} else if len(newTokens) == 1 {
				tree.Set(key, newTokens[0])
			} else {
				tree.Set(key, newTokens)
			}
			if err := writeConfigTree(m.configPath, tree); err != nil {
				return err
			}
		} else {
			return err
		}
	}
	if m.tokens == nil {
		m.tokens = make(map[model.Provider]StringSlice)
	}
	if len(newTokens) > 0 {
		m.tokens[provider] = newTokens
	} else {
		delete(m.tokens, provider)
	}

	return nil
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

func (m *AppFeedManager) UpdateTokens(ctx context.Context, providerStr string, tokens []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var newTokens []string
	for _, tok := range tokens {
		tok = cleanToken(providerStr, tok)
		if tok == "" || strings.Contains(tok, "•") {
			continue
		}
		newTokens = append(newTokens, tok)
	}

	return m.persistTokensLocked(providerStr, newTokens)
}

func (m *AppFeedManager) AddTokens(ctx context.Context, providerStr string, tokens []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var validTokens []string
	for _, tok := range tokens {
		tok = cleanToken(providerStr, tok)
		if tok == "" || strings.Contains(tok, "•") {
			continue
		}
		validTokens = append(validTokens, tok)
	}
	if len(validTokens) == 0 {
		return nil
	}

	provider := model.Provider(providerStr)
	current := m.tokens[provider]
	combined := append([]string(current), validTokens...)

	return m.persistTokensLocked(providerStr, combined)
}

func (m *AppFeedManager) DeleteToken(ctx context.Context, providerStr string, index int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if index < 0 {
		return m.persistTokensLocked(providerStr, nil)
	}

	provider := model.Provider(providerStr)
	current := m.tokens[provider]
	if index >= len(current) {
		return fmt.Errorf("token index %d out of bounds (total tokens: %d)", index, len(current))
	}

	var remaining []string
	for i, tok := range current {
		if i != index {
			remaining = append(remaining, tok)
		}
	}

	return m.persistTokensLocked(providerStr, remaining)
}

func (m *AppFeedManager) GetSystemStats(ctx context.Context) (*web.SystemStats, error) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	m.mu.RLock()
	version := m.version
	commit := m.commit
	buildDate := m.buildDate
	startedAt := m.startedAt
	var dataDir, storageType string
	if m.cfg != nil {
		dataDir = m.cfg.Storage.Local.DataDir
		storageType = m.cfg.Storage.Type
	}
	m.mu.RUnlock()

	total, free, used, _ := getDiskSpace(dataDir)

	_, hasYtDlp := exec.LookPath("yt-dlp")
	_, hasFFmpeg := exec.LookPath("ffmpeg")

	stats := &web.SystemStats{
		Version:    version,
		Commit:     commit,
		BuildDate:  buildDate,
		Uptime:     time.Since(startedAt).Truncate(time.Second).String(),
		StartedAt:  startedAt,
		Goroutines: runtime.NumGoroutine(),
		MemoryMB:   float64(mem.Alloc) / 1024 / 1024,
		DiskTotal:  total,
		DiskFree:   free,
		DiskUsed:   used,
		HasYtDlp:   hasYtDlp == nil,
		HasFFmpeg:  hasFFmpeg == nil,
		DataDir:    dataDir,
		Storage:    storageType,
	}

	return stats, nil
}

func (m *AppFeedManager) UpdateFeed(ctx context.Context, id string, update *web.FeedConfigUpdate) error {
	m.mu.Lock()
	var unlocked bool
	defer func() {
		if !unlocked {
			m.mu.Unlock()
		}
	}()

	f, exists := m.feeds[id]
	if !exists {
		return errors.Errorf("feed %q not found", id)
	}
	resolved, err := update.WithCurrent(f)
	if err != nil {
		return err
	}
	update = resolved
	oldFeed := *f
	updatedFeed := oldFeed
	f = &updatedFeed

	oldLang := strings.TrimSpace(f.Custom.Language)
	newLang := strings.TrimSpace(update.Custom.Language)
	if update.Language != "" && newLang == "" {
		newLang = strings.TrimSpace(update.Language)
		update.Custom.Language = newLang
	}
	languageChanged := (oldLang != newLang) && (oldLang != "" || newLang != "")
	shouldRedownload := !isAudiobookFeed(f) && (update.Redownload || languageChanged)

	if update.PageSize > 0 {
		f.PageSize = update.PageSize
	}
	if update.Format != "" {
		f.Format = model.Format(update.Format)
	}
	if update.Quality != "" {
		f.Quality = model.Quality(update.Quality)
	}
	f.MaxHeight = update.MaxHeight
	f.PlaylistSort = model.Sorting(update.PlaylistSort)
	if update.OPML != nil {
		f.OPML = update.OPML
	}
	f.PrivateFeed = update.PrivateFeed

	if update.KeepLast > 0 {
		f.Clean = &feed.Cleanup{KeepLast: update.KeepLast}
	} else if update.KeepLast == 0 {
		f.Clean = nil
	}

	if update.Language != "" && update.Custom.Language == "" {
		update.Custom.Language = strings.TrimSpace(update.Language)
	}
	f.Filters = update.Filters
	f.Custom = update.Custom
	f.YouTubeDLArgs = update.YouTubeDLArgs
	f.FilenameTemplate = update.FilenameTemplate

	info, parseErr := builder.ParseURL(f.URL)
	if parseErr == nil && info.Provider == model.ProviderAudiobook {
		f.Clean = nil
		f.Format = model.FormatAudio
		f.Quality = model.QualityHigh
		f.MaxHeight = 0
		f.Filters = feed.Filters{}
		f.YouTubeDLArgs = nil
		f.PlaylistSort = ""
	}

	if isAudiobookFeed(f) {
		f.CronSchedule = ""
	} else if update.CronSchedule != "" {
		f.CronSchedule = update.CronSchedule
	}
	if !isAudiobookFeed(f) && f.CronSchedule != "" {
		if _, err := cron.ParseStandard(f.CronSchedule); err != nil {
			return errors.Wrap(err, "invalid update schedule")
		}
	}
	if err := m.checkUpdateCapacityLocked(); err != nil {
		return err
	}
	if err := m.saveFeedToConfig(f); err != nil {
		return err
	}
	m.feeds[id] = f
	if m.updater != nil {
		m.updater.SetFeedConfig(id, f)
	}
	if m.cfg != nil && m.cfg.Feeds != nil {
		m.cfg.Feeds[id] = f
	}

	// Reschedule cron if cron_schedule changed
	if isAudiobookFeed(f) && m.cron != nil {
		if oldID, ok := m.cronEntries[id]; ok {
			m.cron.Remove(oldID)
			delete(m.cronEntries, id)
		}
	}
	if !isAudiobookFeed(f) && update.CronSchedule != "" && update.CronSchedule != oldFeed.CronSchedule {
		if m.cron != nil {
			if oldID, ok := m.cronEntries[id]; ok {
				m.cron.Remove(oldID)
				delete(m.cronEntries, id)
			}
			feedID := id
			cronID, err := m.cron.AddFunc(f.CronSchedule, func() {
				m.QueueScheduledUpdate(feedID)
			})
			if err == nil {
				m.cronEntries[id] = cronID
			} else {
				log.WithError(err).Warnf("failed to re-register cron for feed %q", id)
			}
		}
	}

	m.invalidateStats(id)
	queueErr := m.queueUpdateLocked(f)
	unlocked = true
	m.mu.Unlock()

	if m.db != nil {
		if dbFeed, err := m.db.GetFeedMetadata(ctx, id); err == nil && dbFeed != nil {
			dbFeed.PrivateFeed = f.PrivateFeed
			dbFeed.Episodes = nil // Metadata-only update must not replace a concurrent chapter scan.
			if err := m.db.AddFeed(ctx, id, dbFeed); err != nil {
				log.WithError(err).Warnf("failed to update private feed state for %q", id)
			}
		}
	}

	if shouldRedownload && m.db != nil {
		log.Infof("redownload triggered for feed %q (language changed: %v, explicit redownload: %v, old: %q, new: %q)", id, languageChanged, update.Redownload, oldLang, newLang)
		_ = m.db.WalkEpisodes(ctx, id, func(ep *model.Episode) error {
			if ep.Status == model.EpisodeDownloaded {
				if m.storage != nil {
					_ = m.storage.Delete(ctx, fmt.Sprintf("%s/%s", id, feed.EpisodeName(f, ep)))
					_ = m.storage.Delete(ctx, fmt.Sprintf("%s/%s", id, feed.LegacyEpisodeName(f, ep)))
					for _, ext := range []string{"mp4", "mp3", "m4a", "webm"} {
						_ = m.storage.Delete(ctx, fmt.Sprintf("%s/%s.%s", id, ep.ID, ext))
					}
				}
				_ = m.db.UpdateEpisode(id, ep.ID, func(e *model.Episode) error {
					e.Status = model.EpisodeNew
					e.Size = 0
					e.Error = ""
					e.ErrorAt = nil
					return nil
				})
			}
			return nil
		})
	}

	return queueErr
}

func (m *AppFeedManager) saveFeedToConfig(f *feed.Config) error {
	if m.configPath == "" {
		return nil
	}

	tree, err := toml.LoadFile(m.configPath)
	if err != nil {
		return err
	}

	prefix := fmt.Sprintf("feeds.%s", f.ID)
	tree.Set(prefix+".url", f.URL)
	if f.Format != "" {
		tree.Set(prefix+".format", string(f.Format))
	}
	if f.Quality != "" {
		tree.Set(prefix+".quality", string(f.Quality))
	}
	if f.PageSize > 0 {
		tree.Set(prefix+".page_size", int64(f.PageSize))
	}
	if f.CronSchedule != "" {
		tree.Set(prefix+".cron_schedule", f.CronSchedule)
	} else {
		tree.Delete(prefix + ".cron_schedule")
	}
	if f.MaxHeight > 0 {
		tree.Set(prefix+".max_height", int64(f.MaxHeight))
	} else {
		tree.Delete(prefix + ".max_height")
	}
	if f.PlaylistSort != "" {
		tree.Set(prefix+".playlist_sort", string(f.PlaylistSort))
	} else {
		tree.Delete(prefix + ".playlist_sort")
	}
	if f.OPML != nil {
		tree.Set(prefix+".opml", *f.OPML)
	}
	if f.PrivateFeed {
		tree.Set(prefix+".private_feed", true)
	} else {
		tree.Delete(prefix + ".private_feed")
	}
	if f.Clean != nil && f.Clean.KeepLast > 0 {
		tree.Set(prefix+".clean.keep_last", int64(f.Clean.KeepLast))
	} else {
		tree.Delete(prefix + ".clean")
	}

	// Filters
	if f.Filters.Title != "" {
		tree.Set(prefix+".filters.title", f.Filters.Title)
	} else {
		tree.Delete(prefix + ".filters.title")
	}
	if f.Filters.NotTitle != "" {
		tree.Set(prefix+".filters.not_title", f.Filters.NotTitle)
	} else {
		tree.Delete(prefix + ".filters.not_title")
	}
	if f.Filters.Description != "" {
		tree.Set(prefix+".filters.description", f.Filters.Description)
	} else {
		tree.Delete(prefix + ".filters.description")
	}
	if f.Filters.NotDescription != "" {
		tree.Set(prefix+".filters.not_description", f.Filters.NotDescription)
	} else {
		tree.Delete(prefix + ".filters.not_description")
	}
	if f.Filters.MinDuration > 0 {
		tree.Set(prefix+".filters.min_duration", f.Filters.MinDuration)
	} else {
		tree.Delete(prefix + ".filters.min_duration")
	}
	if f.Filters.MaxDuration > 0 {
		tree.Set(prefix+".filters.max_duration", f.Filters.MaxDuration)
	} else {
		tree.Delete(prefix + ".filters.max_duration")
	}
	if f.Filters.MaxAge > 0 {
		tree.Set(prefix+".filters.max_age", int64(f.Filters.MaxAge))
	} else {
		tree.Delete(prefix + ".filters.max_age")
	}

	// Custom
	if f.Custom.Title != "" {
		tree.Set(prefix+".custom.title", f.Custom.Title)
	} else {
		tree.Delete(prefix + ".custom.title")
	}
	if f.Custom.Description != "" {
		tree.Set(prefix+".custom.description", f.Custom.Description)
	} else {
		tree.Delete(prefix + ".custom.description")
	}
	if f.Custom.Author != "" {
		tree.Set(prefix+".custom.author", f.Custom.Author)
	} else {
		tree.Delete(prefix + ".custom.author")
	}
	if f.Custom.CoverArt != "" {
		tree.Set(prefix+".custom.cover_art", f.Custom.CoverArt)
	} else {
		tree.Delete(prefix + ".custom.cover_art")
	}
	if f.Custom.Category != "" {
		tree.Set(prefix+".custom.category", f.Custom.Category)
	} else {
		tree.Delete(prefix + ".custom.category")
	}
	if f.Custom.Language != "" {
		tree.Set(prefix+".custom.lang", f.Custom.Language)
	} else {
		tree.Delete(prefix + ".custom.lang")
	}
	if f.Custom.Explicit {
		tree.Set(prefix+".custom.explicit", true)
	} else {
		tree.Delete(prefix + ".custom.explicit")
	}

	// Advanced
	if len(f.YouTubeDLArgs) > 0 {
		tree.Set(prefix+".youtube_dl_args", f.YouTubeDLArgs)
	} else {
		tree.Delete(prefix + ".youtube_dl_args")
	}
	if f.FilenameTemplate != "" {
		tree.Set(prefix+".filename_template", f.FilenameTemplate)
	} else {
		tree.Delete(prefix + ".filename_template")
	}
	if f.AudiobookDir != "" {
		tree.Set(prefix+".audiobook_dir", f.AudiobookDir)
	} else {
		tree.Delete(prefix + ".audiobook_dir")
	}

	return writeConfigTree(m.configPath, tree)
}

func (m *AppFeedManager) deleteFeedFromConfig(id string) error {
	if m.configPath == "" {
		return nil
	}

	tree, err := toml.LoadFile(m.configPath)
	if err != nil {
		return err
	}

	_ = tree.Delete(fmt.Sprintf("feeds.%s", id))

	return writeConfigTree(m.configPath, tree)
}

func getFFmpegVersion(ctx context.Context) (string, string) {
	path, err := exec.LookPath("ffmpeg")
	if err != nil {
		return "", ""
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, path, "-version")
	out, err := cmd.Output()
	if err != nil {
		return path, ""
	}

	lines := strings.Split(string(out), "\n")
	if len(lines) > 0 {
		firstLine := strings.TrimSpace(lines[0])
		fields := strings.Fields(firstLine)
		if len(fields) >= 3 && fields[0] == "ffmpeg" && fields[1] == "version" {
			return path, fields[2]
		}
		return path, strings.TrimPrefix(firstLine, "ffmpeg version ")
	}
	return path, ""
}

func (m *AppFeedManager) GetDownloaderConfig(ctx context.Context) (*web.DownloaderConfigInfo, error) {
	m.mu.RLock()
	dl := m.downloader
	var dlCfg ytdl.Config
	if m.cfg != nil {
		dlCfg = m.cfg.Downloader
	}
	var dataDir, storageType string
	if m.cfg != nil {
		dataDir = m.cfg.Storage.Local.DataDir
		storageType = m.cfg.Storage.Type
	}
	m.mu.RUnlock()

	var binPath, binVer string
	ffmpegPath, ffmpegVer := getFFmpegVersion(ctx)

	if dl != nil {
		dlCfg = dl.GetConfig()
		binPath = dl.GetBinaryPath()
		verCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		if ver, err := dl.GetVersion(verCtx); err == nil {
			binVer = strings.TrimSpace(ver)
		}
		cancel()
	}

	cookiesPath := dlCfg.CookiesFile
	if cookiesPath == "" && storageType == "local" && dataDir != "" {
		defaultPath := filepath.Join(dataDir, "cookies.txt")
		if _, err := os.Stat(defaultPath); err == nil {
			cookiesPath = defaultPath
		}
	}

	hasCookies := false
	var cookiesSize int64
	var cookiesLines int
	var cookiesMod string

	if cookiesPath != "" {
		if fi, err := os.Stat(cookiesPath); err == nil {
			hasCookies = true
			cookiesSize = fi.Size()
			cookiesMod = fi.ModTime().Format("2006-01-02 15:04:05")
			if data, err := os.ReadFile(cookiesPath); err == nil {
				cookiesLines = len(strings.Split(string(data), "\n"))
			}
		}
	}

	return &web.DownloaderConfigInfo{
		Timeout:            dlCfg.Timeout,
		SelfUpdate:         dlCfg.SelfUpdate,
		CustomBinary:       dlCfg.CustomBinary,
		CookiesFile:        dlCfg.CookiesFile,
		CookiesFromBrowser: dlCfg.CookiesFromBrowser,
		Proxy:              dlCfg.Proxy,
		Language:           dlCfg.Language,
		HasCookiesFile:     hasCookies,
		CookiesFileSize:    cookiesSize,
		CookiesFileLines:   cookiesLines,
		CookiesLastMod:     cookiesMod,
		BinaryPath:         binPath,
		BinaryVersion:      binVer,
		FFmpegPath:         ffmpegPath,
		FFmpegVersion:      ffmpegVer,
	}, nil
}

func (m *AppFeedManager) UpdateDownloaderConfig(ctx context.Context, update *web.DownloaderConfigUpdate) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if update.Timeout != nil && *update.Timeout > 0 {
		m.cfg.Downloader.Timeout = *update.Timeout
	}
	if update.SelfUpdate != nil {
		m.cfg.Downloader.SelfUpdate = *update.SelfUpdate
	}
	if update.CustomBinary != nil {
		m.cfg.Downloader.CustomBinary = *update.CustomBinary
	}
	if update.CookiesFile != nil {
		m.cfg.Downloader.CookiesFile = *update.CookiesFile
	}
	if update.CookiesFromBrowser != nil {
		m.cfg.Downloader.CookiesFromBrowser = *update.CookiesFromBrowser
	}
	if update.Proxy != nil {
		m.cfg.Downloader.Proxy = *update.Proxy
	}
	if update.Language != nil {
		m.cfg.Downloader.Language = strings.TrimSpace(*update.Language)
	}

	if m.downloader != nil {
		m.downloader.UpdateConfig(m.cfg.Downloader)
	}

	return m.saveDownloaderToConfig(&m.cfg.Downloader)
}

func (m *AppFeedManager) GetCookiesContent(ctx context.Context) (string, error) {
	m.mu.RLock()
	cookiesPath := ""
	if m.cfg != nil {
		cookiesPath = m.cfg.Downloader.CookiesFile
		if cookiesPath == "" && m.cfg.Storage.Type == "local" && m.cfg.Storage.Local.DataDir != "" {
			cookiesPath = filepath.Join(m.cfg.Storage.Local.DataDir, "cookies.txt")
		}
	}
	m.mu.RUnlock()

	if cookiesPath == "" {
		return "", nil
	}

	data, err := os.ReadFile(cookiesPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}

	return string(data), nil
}

func (m *AppFeedManager) UpdateCookiesContent(ctx context.Context, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cookiesPath := m.cfg.Downloader.CookiesFile
	if cookiesPath == "" {
		if m.cfg.Storage.Type == "local" && m.cfg.Storage.Local.DataDir != "" {
			cookiesPath = filepath.Join(m.cfg.Storage.Local.DataDir, "cookies.txt")
		} else {
			cookiesPath = "cookies.txt"
		}
	}

	content = strings.TrimSpace(content)
	if content == "" {
		// Remove cookies file
		_ = os.Remove(cookiesPath)
		m.cfg.Downloader.CookiesFile = ""
		if m.downloader != nil {
			m.downloader.SetCookiesFile("")
		}
		m.syncCookiesToFeeds("")
		return m.saveDownloaderToConfig(&m.cfg.Downloader)
	}

	// Ensure parent directory exists
	dir := filepath.Dir(cookiesPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return errors.Wrap(err, "failed to create cookies directory")
		}
	}

	if err := os.WriteFile(cookiesPath, []byte(content+"\n"), 0600); err != nil {
		return errors.Wrap(err, "failed to save cookies file")
	}

	m.cfg.Downloader.CookiesFile = cookiesPath
	if m.downloader != nil {
		m.downloader.SetCookiesFile(cookiesPath)
	}

	m.syncCookiesToFeeds(cookiesPath)

	return m.saveDownloaderToConfig(&m.cfg.Downloader)
}

func (m *AppFeedManager) syncCookiesToFeeds(newCookiesPath string) {
	for feedID, feedCfg := range m.cfg.Feeds {
		if len(feedCfg.YouTubeDLArgs) == 0 {
			continue
		}
		hasCookies := false
		newArgs := make([]string, 0, len(feedCfg.YouTubeDLArgs))
		for i := 0; i < len(feedCfg.YouTubeDLArgs); i++ {
			arg := feedCfg.YouTubeDLArgs[i]
			if arg == "--cookies" {
				if i+1 < len(feedCfg.YouTubeDLArgs) {
					if newCookiesPath != "" {
						newArgs = append(newArgs, "--cookies", newCookiesPath)
					}
					i++ // skip old path argument
					hasCookies = true
					continue
				}
			} else if strings.HasPrefix(arg, "--cookies=") {
				if newCookiesPath != "" {
					newArgs = append(newArgs, "--cookies="+newCookiesPath)
				}
				hasCookies = true
				continue
			}
			newArgs = append(newArgs, arg)
		}

		if hasCookies {
			feedCfg.YouTubeDLArgs = newArgs
			m.cfg.Feeds[feedID] = feedCfg
			_ = m.saveFeedToConfig(feedCfg)
			log.Infof("updated --cookies path in feed %q to %q", feedID, newCookiesPath)
		}
	}
}

func (m *AppFeedManager) TestDownloader(ctx context.Context, testURL string) (*web.TestDownloaderResult, error) {
	if m.downloader == nil {
		return nil, errors.New("downloader not initialized")
	}

	out, err := m.downloader.TestURL(ctx, testURL)
	if err != nil {
		cleanErr := out
		if cleanErr == "" {
			cleanErr = err.Error()
		}
		return &web.TestDownloaderResult{
			Success: false,
			Error:   cleanErr,
			Output:  out,
		}, nil
	}

	// Try parsing JSON metadata
	var meta struct {
		Title    string `json:"title"`
		Uploader string `json:"uploader"`
		Channel  string `json:"channel"`
	}
	_ = json.Unmarshal([]byte(out), &meta)

	channel := meta.Channel
	if channel == "" {
		channel = meta.Uploader
	}

	return &web.TestDownloaderResult{
		Success: true,
		Title:   meta.Title,
		Channel: channel,
		Output:  out,
	}, nil
}

func (m *AppFeedManager) saveDownloaderToConfig(cfg *ytdl.Config) error {
	if m.configPath == "" {
		return nil
	}

	tree, err := toml.LoadFile(m.configPath)
	if err != nil {
		return err
	}

	if cfg.Timeout > 0 {
		tree.Set("downloader.timeout", int64(cfg.Timeout))
	}
	tree.Set("downloader.self_update", cfg.SelfUpdate)
	if cfg.CustomBinary != "" {
		tree.Set("downloader.custom_binary", cfg.CustomBinary)
	} else {
		tree.Delete("downloader.custom_binary")
	}
	if cfg.CookiesFile != "" {
		tree.Set("downloader.cookies_file", cfg.CookiesFile)
	} else {
		tree.Delete("downloader.cookies_file")
	}
	if cfg.CookiesFromBrowser != "" {
		tree.Set("downloader.cookies_from_browser", cfg.CookiesFromBrowser)
	} else {
		tree.Delete("downloader.cookies_from_browser")
	}
	if cfg.Proxy != "" {
		tree.Set("downloader.proxy", cfg.Proxy)
	} else {
		tree.Delete("downloader.proxy")
	}
	if cfg.Language != "" {
		tree.Set("downloader.language", cfg.Language)
	} else {
		tree.Delete("downloader.language")
	}

	return writeConfigTree(m.configPath, tree)
}

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

func (m *AppFeedManager) ChangePassword(ctx context.Context, oldPassword, newPassword string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	currentPassword := m.cfg.Server.Admin.Password
	if currentPassword != "" && currentPassword != oldPassword {
		return errors.New("current password does not match")
	}

	if strings.TrimSpace(newPassword) == "" {
		return errors.New("new password cannot be empty")
	}
	if len(newPassword) < 4 {
		return errors.New("new password must be at least 4 characters long")
	}

	if m.configPath != "" {
		tree, err := toml.LoadFile(m.configPath)
		if err != nil {
			return errors.Wrap(err, "failed to load config file")
		}
		tree.Set("server.admin.password", newPassword)

		if err := writeConfigTree(m.configPath, tree); err != nil {
			return errors.Wrap(err, "failed to save config file")
		}
	}
	m.cfg.Server.Admin.Password = newPassword

	return nil
}

func (m *AppFeedManager) UpdateAdminSecurity(ctx context.Context, maxAttempts int, window, lockout string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.configPath != "" {
		tree, err := toml.LoadFile(m.configPath)
		if err != nil {
			return errors.Wrap(err, "failed to load config file")
		}
		tree.Set("server.admin.max_attempts", int64(maxAttempts))
		tree.Set("server.admin.window", window)
		tree.Set("server.admin.lockout", lockout)

		if err := writeConfigTree(m.configPath, tree); err != nil {
			return errors.Wrap(err, "failed to save config file")
		}
	}

	m.cfg.Server.Admin.MaxAttempts = maxAttempts
	m.cfg.Server.Admin.WindowPeriod = window
	m.cfg.Server.Admin.LockoutPeriod = lockout

	return nil
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

func (m *AppFeedManager) audiobooksDirLocked() string {
	if m.cfg != nil && m.cfg.Audiobooks.Dir != "" {
		return m.cfg.Audiobooks.Dir
	}
	dir := os.Getenv("PODSYNC_AUDIOBOOKS_DIR")
	if dir != "" {
		return dir
	}
	return "./audiobooks"
}

func (m *AppFeedManager) ListAudiobookDirs(ctx context.Context) ([]web.AudiobookDirInfo, error) {
	m.mu.RLock()
	baseDir := m.audiobooksDirLocked()
	exts := map[string]bool{
		".mp3": true, ".m4b": true, ".m4a": true, ".aac": true, ".ogg": true, ".opus": true, ".flac": true,
	}
	if m.cfg != nil && len(m.cfg.Audiobooks.SupportedExtensions) > 0 {
		exts = make(map[string]bool)
		for _, e := range m.cfg.Audiobooks.SupportedExtensions {
			exts[strings.ToLower(e)] = true
		}
	}

	coverNames := map[string]bool{
		"cover.jpg": true, "cover.png": true, "folder.jpg": true, "cover.jpeg": true,
	}
	if m.cfg != nil && len(m.cfg.Audiobooks.CoverFilenames) > 0 {
		coverNames = make(map[string]bool)
		for _, c := range m.cfg.Audiobooks.CoverFilenames {
			coverNames[strings.ToLower(c)] = true
		}
	}

	// Map existing feeds for quick lookup
	linkedDirs := make(map[string]string)
	for _, f := range m.feeds {
		info, err := builder.ParseURL(f.URL)
		if err == nil && info.Provider == model.ProviderAudiobook {
			linkedDirs[info.ItemID] = f.ID
			linkedDirs[filepath.Base(info.ItemID)] = f.ID
		}
	}
	m.mu.RUnlock()

	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, errors.Wrapf(err, "failed to access audiobooks directory: %s", baseDir)
	}

	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read audiobooks directory: %s", baseDir)
	}

	results := make([]web.AudiobookDirInfo, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dirName := entry.Name()
		if strings.HasPrefix(dirName, ".") {
			continue
		}

		fullPath := filepath.Join(baseDir, dirName)
		files, err := os.ReadDir(fullPath)
		if err != nil {
			continue
		}

		fileCount := 0
		var totalSize int64
		hasCover := false

		for _, f := range files {
			if f.IsDir() {
				continue
			}
			lowerName := strings.ToLower(f.Name())
			if coverNames[lowerName] {
				hasCover = true
			}
			ext := strings.ToLower(filepath.Ext(lowerName))
			if exts[ext] {
				fileCount++
				if info, err := f.Info(); err == nil {
					totalSize += info.Size()
				}
			}
		}

		feedID, isLinked := linkedDirs[dirName]

		var suggestedAuthor, suggestedTitle string
		if parts := strings.SplitN(dirName, " - ", 2); len(parts) == 2 {
			suggestedAuthor = strings.TrimSpace(parts[0])
			suggestedTitle = strings.TrimSpace(parts[1])
		} else {
			suggestedTitle = dirName
		}

		results = append(results, web.AudiobookDirInfo{
			Name:            dirName,
			Path:            fullPath,
			FileCount:       fileCount,
			HasCover:        hasCover,
			TotalSize:       totalSize,
			Linked:          isLinked,
			FeedID:          feedID,
			SuggestedTitle:  suggestedTitle,
			SuggestedAuthor: suggestedAuthor,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return strings.ToLower(results[i].Name) < strings.ToLower(results[j].Name)
	})

	return results, nil
}

var cyrillicToLatinMap = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "yo", 'ж': "zh",
	'з': "z", 'и': "i", 'й': "y", 'к': "k", 'л': "l", 'м': "m", 'н': "n", 'о': "o",
	'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u", 'ф': "f", 'х': "kh", 'ц': "ts",
	'ч': "ch", 'ш': "sh", 'щ': "shch", 'ъ': "", 'ы': "y", 'ь': "", 'э': "e", 'ю': "yu",
	'я': "ya", 'є': "ye", 'і': "i", 'ї': "yi", 'ґ': "g", 'ў': "u",
}

func transliterateCyrillic(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if lat, ok := cyrillicToLatinMap[r]; ok {
			b.WriteString(lat)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func generateAudiobookSlug(s string) string {
	s = transliterateCyrillic(s)
	s = strings.ToLower(strings.TrimSpace(s))
	reg := regexp.MustCompile(`[^a-z0-9_-]+`)
	slug := reg.ReplaceAllString(s, "-")
	slug = strings.Trim(slug, "-")
	if len(slug) > 50 {
		slug = slug[:50]
		slug = strings.Trim(slug, "-")
	}
	return slug
}

func (m *AppFeedManager) ImportAudiobook(ctx context.Context, req *web.ImportAudiobookRequest) (*feed.Config, error) {
	if req == nil {
		return nil, errors.New("empty request")
	}
	dirName := strings.TrimSpace(req.DirName)
	if dirName == "" {
		return nil, errors.New("dir_name is required")
	}

	cleanedDir := filepath.Clean(dirName)
	if strings.HasPrefix(cleanedDir, "..") || strings.Contains(cleanedDir, string(filepath.Separator)+"..") {
		return nil, errors.New("invalid directory path")
	}

	id := strings.TrimSpace(req.ID)
	if id == "" {
		base := req.Title
		if base == "" {
			base = dirName
		}
		id = generateAudiobookSlug(base)
	}

	if id == "" {
		return nil, errors.New("could not generate valid feed ID")
	}
	if !regexp.MustCompile(`^[a-zA-Z0-9_-]+$`).MatchString(id) {
		return nil, errors.New("feed id must contain only English letters (a-z, A-Z), numbers, dashes, and underscores")
	}

	m.mu.RLock()
	_, exists := m.feeds[id]
	m.mu.RUnlock()
	if exists {
		return nil, errors.Errorf("feed with ID %q already exists", id)
	}

	baseDir := m.GetAudiobooksDir()
	if filepath.IsAbs(cleanedDir) || cleanedDir == "." || cleanedDir == ".." || strings.HasPrefix(cleanedDir, ".."+string(filepath.Separator)) {
		return nil, errors.New("book must be a subdirectory of the audiobook library")
	}
	root, err := os.OpenRoot(baseDir)
	if err != nil {
		return nil, errors.Wrap(err, "cannot access audiobook library")
	}
	defer root.Close()

	stat, err := root.Stat(cleanedDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errors.Errorf("audiobook folder %q does not exist", cleanedDir)
		}
		return nil, errors.Wrap(err, "cannot access audiobook folder")
	}
	if !stat.IsDir() {
		return nil, errors.Errorf("%q is not a directory", cleanedDir)
	}
	fullPath := filepath.Join(baseDir, cleanedDir)

	newFeed := &feed.Config{
		ID:           id,
		URL:          fmt.Sprintf("audiobook://%s", cleanedDir),
		Format:       model.FormatAudio,
		Quality:      model.QualityHigh,
		PlaylistSort: model.SortingAsc,
		PageSize:     1000,
		AudiobookDir: fullPath,
	}

	if req.Title != "" || req.Author != "" || req.Description != "" {
		newFeed.Custom = feed.Custom{
			Title:       req.Title,
			Author:      req.Author,
			Description: req.Description,
		}
	}
	if req.PrivateFeed {
		newFeed.PrivateFeed = true
	}

	abBuilder, err := builder.NewAudiobookBuilder(baseDir)
	if err != nil {
		return nil, err
	}
	if _, err := abBuilder.Build(ctx, newFeed); err != nil {
		return nil, errors.Wrap(err, "cannot import audiobook")
	}
	if err := m.AddFeed(ctx, newFeed); err != nil {
		return nil, err
	}

	return newFeed, nil
}

func (m *AppFeedManager) SaveAudiobookFile(bookDir string, filename string, r io.Reader) error {
	baseDir := m.GetAudiobooksDir()
	if baseDir == "" {
		return errors.New("audiobooks directory is not configured")
	}
	cleanBookDir := filepath.Clean(bookDir)
	if cleanBookDir == "." || filepath.IsAbs(cleanBookDir) || cleanBookDir == ".." || strings.HasPrefix(cleanBookDir, ".."+string(filepath.Separator)) {
		return errors.New("invalid book directory")
	}
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return errors.Wrap(err, "failed to create audiobooks base directory")
	}
	root, err := os.OpenRoot(baseDir)
	if err != nil {
		return errors.Wrap(err, "failed to open audiobooks directory")
	}
	defer root.Close()
	if err := root.MkdirAll(cleanBookDir, 0755); err != nil {
		return errors.Wrap(err, "failed to create book directory")
	}

	cleanFilename := filepath.Base(filepath.Clean(filename))
	if cleanFilename == "." || cleanFilename == "/" || cleanFilename == ".." {
		return errors.New("invalid filename")
	}

	targetPath := filepath.Join(cleanBookDir, cleanFilename)
	out, err := root.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if os.IsExist(err) {
		// Retrying an interrupted multi-file upload may include completed files.
		existing, openErr := root.Open(targetPath)
		if openErr != nil {
			return openErr
		}
		defer existing.Close()
		oldHash, newHash := sha256.New(), sha256.New()
		oldSize, readErr := io.Copy(oldHash, existing)
		if readErr != nil {
			return readErr
		}
		newSize, readErr := io.Copy(newHash, r)
		if readErr != nil {
			return readErr
		}
		if oldSize == newSize && bytes.Equal(oldHash.Sum(nil), newHash.Sum(nil)) {
			return nil
		}
		return errors.New("file already exists with different content; choose a different filename")
	}
	if err != nil {
		return errors.Wrap(err, "failed to create destination file")
	}
	if _, err := io.Copy(out, r); err != nil {
		_ = out.Close()
		_ = root.Remove(targetPath)
		return errors.Wrap(err, "failed to write file")
	}
	if err := out.Close(); err != nil {
		_ = root.Remove(targetPath)
		return err
	}
	return nil
}

func (m *AppFeedManager) DeleteAudiobookDir(ctx context.Context, dirName string) error {
	dirName = strings.TrimSpace(dirName)
	if dirName == "" {
		return errors.New("dir_name is required")
	}

	cleanDir := filepath.Clean(dirName)
	if cleanDir == "." || cleanDir == "/" || cleanDir == ".." || strings.HasPrefix(cleanDir, ".."+string(filepath.Separator)) || filepath.IsAbs(cleanDir) {
		return errors.New("invalid directory path")
	}

	baseDir := m.GetAudiobooksDir()
	if baseDir == "" {
		return errors.New("audiobooks directory is not configured")
	}

	root, err := os.OpenRoot(baseDir)
	if err != nil {
		return errors.Wrap(err, "failed to open audiobooks root directory")
	}
	defer root.Close()

	stat, err := root.Stat(cleanDir)
	if err != nil {
		if os.IsNotExist(err) {
			return errors.Errorf("audiobook directory %q not found", dirName)
		}
		return err
	}
	if !stat.IsDir() {
		return errors.Errorf("%q is not a directory", dirName)
	}

	targetPath := filepath.Join(baseDir, cleanDir)

	// If this directory is linked to any active feed(s), delete the feed(s) cleanly
	m.mu.Lock()
	var linkedFeedIDs []string
	for feedID, f := range m.feeds {
		info, parseErr := builder.ParseURL(f.URL)
		if parseErr == nil && info.Provider == model.ProviderAudiobook && (info.ItemID == cleanDir || f.AudiobookDir == targetPath) {
			linkedFeedIDs = append(linkedFeedIDs, feedID)
		}
	}
	m.mu.Unlock()

	for _, feedID := range linkedFeedIDs {
		log.Infof("deleting linked audiobook feed %q before deleting folder %q", feedID, dirName)
		if delErr := m.DeleteFeed(ctx, feedID, true); delErr != nil {
			log.WithError(delErr).Warnf("failed to delete linked feed %q during folder deletion", feedID)
		}
	}

	// Remove folder from disk via root
	if err := root.RemoveAll(cleanDir); err != nil {
		return errors.Wrapf(err, "failed to delete audiobook directory %q", dirName)
	}

	log.Infof("successfully deleted audiobook directory from disk: %s", targetPath)
	return nil
}

func (m *AppFeedManager) SaveCoverImage(ctx context.Context, feedID string, filename string, r io.Reader) (*web.CoverUploadResult, error) {
	m.mu.RLock()
	dataDir := ""
	if m.cfg != nil {
		dataDir = m.cfg.Storage.Local.DataDir
	}
	publicURL := web.Config{}.PublicURL()
	if m.cfg != nil {
		publicURL = m.cfg.Server.PublicURL()
	}
	m.mu.RUnlock()

	if dataDir == "" {
		return nil, errors.New("storage data directory is not configured")
	}

	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif":
		// valid
	default:
		ext = ".jpg"
	}

	var targetDir, targetFilename, relURL string
	cleanFeedID := strings.TrimSpace(feedID)

	if cleanFeedID != "" {
		baseID := filepath.Base(filepath.Clean(cleanFeedID))
		if baseID == "." || baseID == "/" || baseID == ".." || strings.Contains(cleanFeedID, "..") {
			return nil, errors.New("invalid feed ID")
		}

		mountedDir := ""
		if m.storage != nil {
			if mounter, ok := m.storage.(fs.Mounter); ok {
				if dir, ok := mounter.GetMount(baseID); ok && dir != "" {
					mountedDir = dir
				}
			}
		}

		if mountedDir != "" {
			targetDir = mountedDir
		} else {
			targetDir = filepath.Join(dataDir, baseID)
		}

		if err := os.MkdirAll(targetDir, 0755); err != nil {
			return nil, errors.Wrap(err, "failed to create feed cover directory")
		}

		targetFilename = "cover" + ext
		relURL = fmt.Sprintf("/%s/%s", baseID, targetFilename)
	} else {
		targetDir = filepath.Join(dataDir, "covers")
		if err := os.MkdirAll(targetDir, 0755); err != nil {
			return nil, errors.Wrap(err, "failed to create covers directory")
		}

		targetFilename = fmt.Sprintf("cover_%d%s", time.Now().UnixNano(), ext)
		relURL = fmt.Sprintf("/covers/%s", targetFilename)
	}

	targetPath := filepath.Join(targetDir, targetFilename)
	out, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return nil, errors.Wrap(err, "failed to open cover target file")
	}
	defer out.Close()

	if _, err := io.Copy(out, r); err != nil {
		return nil, errors.Wrap(err, "failed to write cover file")
	}

	fullURL := strings.TrimRight(publicURL, "/") + relURL
	return &web.CoverUploadResult{
		URL:      fullURL,
		RelURL:   relURL,
		FeedID:   cleanFeedID,
		Filename: targetFilename,
	}, nil
}
