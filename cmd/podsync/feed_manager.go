package main

// Core feed manager: lifecycle, feed status, and the shared helpers that
// load, sort and link episodes for the library and detail views.

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mxpv/podsync/pkg/builder"
	"github.com/mxpv/podsync/pkg/db"
	"github.com/mxpv/podsync/pkg/feed"
	"github.com/mxpv/podsync/pkg/fs"
	"github.com/mxpv/podsync/pkg/jobqueue"
	"github.com/mxpv/podsync/pkg/logbuffer"
	"github.com/mxpv/podsync/pkg/model"
	"github.com/mxpv/podsync/pkg/personal"
	"github.com/mxpv/podsync/pkg/selfupdate"
	"github.com/mxpv/podsync/pkg/ytdl"
	"github.com/mxpv/podsync/services/update"
	"github.com/mxpv/podsync/services/web"
	"github.com/pkg/errors"
	"github.com/robfig/cron/v3"
	log "github.com/sirupsen/logrus"
)

// validFeedIDPattern matches identifiers that are safe in URLs, file names and
// BadgerDB keys. Compiled once because it is used on every feed mutation.
var validFeedIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

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
	configWriteMu  sync.Mutex
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
	updateCancels  map[string]context.CancelFunc
	updateWaiters  map[string]chan struct{}
	coverCache     sync.Map
	statsMu        sync.RWMutex
	statsCache     map[string]feedStats
	queue          *jobqueue.JobQueue
	selfUpdater    *selfupdate.Updater
	restart        func() error
	selfUpdateMu   sync.Mutex
	selfUpdating   bool
	personalFeeds  *personal.Service
}

var cyrillicToLatinMap = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "yo", 'ж': "zh",
	'з': "z", 'и': "i", 'й': "y", 'к': "k", 'л': "l", 'м': "m", 'н': "n", 'о': "o",
	'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u", 'ф': "f", 'х': "kh", 'ц': "ts",
	'ч': "ch", 'ш': "sh", 'щ': "shch", 'ъ': "", 'ы': "y", 'ь': "", 'э': "e", 'ю': "yu",
	'я': "ya", 'є': "ye", 'і': "i", 'ї': "yi", 'ґ': "g", 'ў': "u",
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
		updateCancels:  make(map[string]context.CancelFunc),
		updateWaiters:  make(map[string]chan struct{}),
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
	m.mu.RUnlock()
	if !ok {
		return nil, nil, errors.Errorf("feed %q not found", id)
	}

	summary := m.buildSummary(ctx, f)
	episodes, audiobook, fromDisk, err := m.feedEpisodes(ctx, f)
	if err != nil {
		return nil, nil, err
	}
	if fromDisk {
		summary.EpisodeCount = len(episodes)
		summary.DownloadedCount = len(episodes)
	}

	if audiobook || f.PlaylistSort == model.SortingAsc {
		sortEpisodes(episodes, "date-asc")
	} else {
		sortEpisodes(episodes, "date-desc")
	}
	m.resolveEpisodeMediaURLs(ctx, f, episodes, episodeMediaBase(summary, id), audiobook)
	return &summary, episodes, nil
}

func (m *AppFeedManager) GetFeedEpisodes(ctx context.Context, id string, query web.EpisodeQuery) (*web.EpisodePageResult, error) {
	m.mu.RLock()
	f, ok := m.feeds[id]
	m.mu.RUnlock()
	if !ok {
		return nil, errors.Errorf("feed %q not found", id)
	}

	allEpisodes, audiobook, _, err := m.feedEpisodes(ctx, f)
	if err != nil {
		return nil, err
	}
	totalCount := len(allEpisodes)

	search := strings.TrimSpace(strings.ToLower(query.Search))
	status := strings.TrimSpace(strings.ToLower(query.Status))
	duration := strings.TrimSpace(strings.ToLower(query.Duration))
	statusCounts := make(map[model.EpisodeStatus]int)

	filtered := make([]*model.Episode, 0, len(allEpisodes))
	for _, ep := range allEpisodes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		statusCounts[ep.Status]++
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
		if audiobook || f.PlaylistSort == model.SortingAsc {
			sortOrder = "date-asc"
		} else {
			sortOrder = "date-desc"
		}
	}
	sortEpisodes(filtered, sortOrder)

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
	if offset >= matchingCount {
		offset = 0
		if matchingCount > 0 {
			offset = ((matchingCount - 1) / limit) * limit
		}
	}

	end := offset + limit
	if end > matchingCount {
		end = matchingCount
	}

	paged := append([]*model.Episode{}, filtered[offset:end]...)
	hasMore := end < matchingCount
	nextOffset := 0
	if hasMore {
		nextOffset = end
	}

	summary := m.buildSummary(ctx, f)
	m.resolveEpisodeMediaURLs(ctx, f, paged, episodeMediaBase(summary, id), audiobook)

	return &web.EpisodePageResult{
		Feed:         &summary,
		StatusCounts: statusCounts,
		Episodes:     paged,
		Total:        totalCount,
		Matching:     matchingCount,
		Offset:       offset,
		Limit:        limit,
		HasMore:      hasMore,
		NextOffset:   nextOffset,
	}, nil
}

// extractCoverArtFromXML reads the generated <feed_id>.xml from storage and
// returns the channel cover. The feed is parsed as a stream so the cover is
// found without loading an arbitrarily large RSS document into memory.

// extractCoverArtFromXML reads the generated <feed_id>.xml from storage and
// returns the channel cover. The feed is parsed as a stream so the cover is
// found without loading an arbitrarily large RSS document into memory.
func (m *AppFeedManager) extractCoverArtFromXML(feedID string) string {
	if val, ok := m.coverCache.Load(feedID); ok {
		if cover, ok := val.(string); ok {
			return cover
		}
	}

	if m.storage == nil {
		return ""
	}
	file, err := m.storage.Open(fmt.Sprintf("%s.xml", feedID))
	if err != nil || file == nil {
		return ""
	}
	defer file.Close()

	// Cache misses too: without a cover, every library refresh would otherwise
	// issue another storage read (a network request on S3). The entry is
	// invalidated whenever the feed is rebuilt.
	cover := feedCoverFromRSS(io.LimitReader(file, 2<<20))
	m.coverCache.Store(feedID, cover)
	return cover
}

// feedCoverFromRSS scans an RSS document for the channel image. It supports
// both the standard <image><url> element and <itunes:image href="...">.

// feedCoverFromRSS scans an RSS document for the channel image. It supports
// both the standard <image><url> element and <itunes:image href="...">.
func feedCoverFromRSS(r io.Reader) string {
	decoder := xml.NewDecoder(r)
	inImage := false
	for {
		token, err := decoder.Token()
		if err != nil {
			return ""
		}
		switch element := token.(type) {
		case xml.StartElement:
			switch element.Name.Local {
			case "image":
				inImage = true
				for _, attr := range element.Attr {
					if attr.Name.Local == "href" {
						if value := strings.TrimSpace(attr.Value); value != "" {
							return value
						}
					}
				}
			case "url":
				if !inImage {
					continue
				}
				var value string
				if err := decoder.DecodeElement(&value, &element); err != nil {
					return ""
				}
				if value = strings.TrimSpace(value); value != "" {
					return value
				}
			}
		case xml.EndElement:
			if element.Name.Local == "image" {
				inImage = false
			}
		}
	}
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
		ID:             f.ID,
		URL:            f.URL,
		Format:         string(f.Format),
		Quality:        string(f.Quality),
		PageSize:       f.PageSize,
		CronSchedule:   f.CronSchedule,
		UpdatePeriod:   f.UpdatePeriod.String(),
		PrivateFeed:    f.PrivateFeed,
		IgnoreLive:     f.IgnoreLive,
		NormalizeAudio: f.NormalizeAudio,
		Proxy:          f.Proxy,
		Updating:       updating,
		Config:         f,
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
	summary.PocketCastsURL = fmt.Sprintf("https://pocketcasts.com/follow/%s", feedURL)
	summary.OvercastURL = fmt.Sprintf("overcast://x-callback-url/add?url=%s", url.QueryEscape(feedURL))

	return summary
}

// Audiobooks are refreshed on import, settings changes, and manual updates.

// Audiobooks are refreshed on import, settings changes, and manual updates.
func isAudiobookFeed(f *feed.Config) bool {
	if strings.HasPrefix(f.URL, "audiobook://") {
		return true
	}
	info, err := builder.ParseURL(f.URL)
	return err == nil && info.Provider == model.ProviderAudiobook
}

// audiobooksDir resolves the configured audiobook root using the same defaults
// as the audiobook builder.

// audiobooksDir resolves the configured audiobook root using the same defaults
// as the audiobook builder.
func (m *AppFeedManager) audiobooksDir() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.audiobooksDirLocked()
}

// feedEpisodes loads persisted episodes for a feed. Audiobooks that have never
// been synchronized are discovered from disk so the library and detail views
// stay usable before their first update. fromDisk reports that fallback.

// feedEpisodes loads persisted episodes for a feed. Audiobooks that have never
// been synchronized are discovered from disk so the library and detail views
// stay usable before their first update. fromDisk reports that fallback.
func (m *AppFeedManager) feedEpisodes(ctx context.Context, f *feed.Config) (episodes []*model.Episode, audiobook bool, fromDisk bool, err error) {
	episodes = make([]*model.Episode, 0)
	if err := m.db.WalkEpisodes(ctx, f.ID, func(episode *model.Episode) error {
		copy := *episode
		episodes = append(episodes, &copy)
		return nil
	}); err != nil {
		return nil, false, false, errors.Wrap(err, "failed to load episodes")
	}

	audiobook = isAudiobookFeed(f)
	if len(episodes) == 0 && audiobook {
		if audiobookBuilder, buildErr := builder.NewAudiobookBuilder(m.audiobooksDir()); buildErr == nil {
			if built, buildErr := audiobookBuilder.Build(ctx, f); buildErr == nil && built != nil {
				episodes = built.Episodes
				fromDisk = true
			}
		}
	}
	return episodes, audiobook, fromDisk, nil
}

// episodeStats aggregates the counters shown on feed cards and detail views.

// episodeStats aggregates the counters shown on feed cards and detail views.
func episodeStats(episodes []*model.Episode) (total, downloaded, failed int, size int64) {
	for _, episode := range episodes {
		total++
		switch episode.Status {
		case model.EpisodeDownloaded:
			downloaded++
			size += episode.Size
		case model.EpisodeError:
			failed++
		}
	}
	return total, downloaded, failed, size
}

// episodeMediaBase is the URL prefix shared by every enclosure in a feed.

// episodeMediaBase is the URL prefix shared by every enclosure in a feed.
func episodeMediaBase(summary web.FeedSummary, feedID string) string {
	return strings.TrimSuffix(summary.FeedURL, feedID+".xml") + url.PathEscape(feedID) + "/"
}

// resolveEpisodeMediaURLs fills inline media links for downloaded episodes.
// Audiobook chapters are served from the virtual mount under their ID, while
// regular feeds keep the configured filename template and its legacy fallback.

// resolveEpisodeMediaURLs fills inline media links for downloaded episodes.
// Audiobook chapters are served from the virtual mount under their ID, while
// regular feeds keep the configured filename template and its legacy fallback.
func (m *AppFeedManager) resolveEpisodeMediaURLs(ctx context.Context, f *feed.Config, episodes []*model.Episode, base string, audiobook bool) {
	for _, episode := range episodes {
		if episode.Status != model.EpisodeDownloaded {
			continue
		}
		filename := feed.EpisodeName(f, episode)
		if audiobook {
			filename = episode.ID
		} else if m.storage != nil {
			if _, err := m.storage.Size(ctx, f.ID+"/"+filename); err != nil {
				legacy := feed.LegacyEpisodeName(f, episode)
				if _, legacyErr := m.storage.Size(ctx, f.ID+"/"+legacy); legacyErr == nil {
					filename = legacy
				}
			}
		}
		episode.MediaURL = base + url.PathEscape(filename)
	}
}

// sortEpisodes implements the ordering options exposed by the Web UI. Ties are
// broken by natural episode-ID order so pagination remains deterministic.

// sortEpisodes implements the ordering options exposed by the Web UI. Ties are
// broken by natural episode-ID order so pagination remains deterministic.
func sortEpisodes(episodes []*model.Episode, order string) {
	switch order {
	case "date-asc":
		sort.Slice(episodes, func(i, j int) bool {
			if episodes[i].PubDate.Equal(episodes[j].PubDate) {
				return builder.NaturalLess(episodes[i].ID, episodes[j].ID)
			}
			return episodes[i].PubDate.Before(episodes[j].PubDate)
		})
	case "title-asc":
		sort.Slice(episodes, func(i, j int) bool {
			ti, tj := episodes[i].Title, episodes[j].Title
			if ti == "" {
				ti = episodes[i].ID
			}
			if tj == "" {
				tj = episodes[j].ID
			}
			if strings.EqualFold(ti, tj) {
				return builder.NaturalLess(episodes[i].ID, episodes[j].ID)
			}
			return strings.ToLower(ti) < strings.ToLower(tj)
		})
	case "duration-desc":
		sort.Slice(episodes, func(i, j int) bool {
			if episodes[i].Duration == episodes[j].Duration {
				return builder.NaturalLess(episodes[i].ID, episodes[j].ID)
			}
			return episodes[i].Duration > episodes[j].Duration
		})
	case "duration-asc":
		sort.Slice(episodes, func(i, j int) bool {
			if episodes[i].Duration == episodes[j].Duration {
				return builder.NaturalLess(episodes[i].ID, episodes[j].ID)
			}
			return episodes[i].Duration < episodes[j].Duration
		})
	default: // "date-desc"
		sort.Slice(episodes, func(i, j int) bool {
			if episodes[i].PubDate.Equal(episodes[j].PubDate) {
				return builder.NaturalLess(episodes[i].ID, episodes[j].ID)
			}
			return episodes[i].PubDate.After(episodes[j].PubDate)
		})
	}
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

func isCachedStatsValid(stats *feedStats) bool {
	if stats == nil || stats.episodeCount <= 0 || stats.totalSize < 0 {
		return false
	}
	if stats.downloadedCount == 0 && stats.totalSize > 0 {
		return false
	}
	return true
}

func (m *AppFeedManager) getFeedStats(ctx context.Context, f *feed.Config) (int, int, int, int64) {
	m.statsMu.RLock()
	cached, ok := m.statsCache[f.ID]
	m.statsMu.RUnlock()
	if ok && time.Since(cached.updatedAt) < 15*time.Second && isCachedStatsValid(&cached) {
		return cached.episodeCount, cached.downloadedCount, cached.errorCount, cached.totalSize
	}

	var epCount, dlCount, errCount int
	var totalSize int64
	if m.db != nil {
		needsRecount := true
		if stats, err := m.db.GetFeedStats(ctx, f.ID); err == nil && stats != nil {
			epCount = stats.EpisodeCount
			dlCount = stats.DownloadedCount
			errCount = stats.ErrorCount
			totalSize = stats.TotalSize
			needsRecount = epCount == 0 || (dlCount == 0 && totalSize > 0) || totalSize < 0
		}
		if needsRecount {
			var episodes []*model.Episode
			_ = m.db.WalkEpisodes(ctx, f.ID, func(ep *model.Episode) error {
				episodes = append(episodes, ep)
				return nil
			})
			if len(episodes) > 0 {
				epCount, dlCount, errCount, totalSize = episodeStats(episodes)
			}
		}
	}
	// Audiobooks can be available from the mounted folder before their first
	// update is persisted in Badger. Keep their library card in sync with the
	// detail view, which already discovers chapters from disk in that case.
	if epCount == 0 && isAudiobookFeed(f) {
		episodes, _, fromDisk, _ := m.feedEpisodes(ctx, f)
		if fromDisk {
			epCount, dlCount, errCount, totalSize = episodeStats(episodes)
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

func (m *AppFeedManager) TryStartFeedUpdateWithCancel(parentCtx context.Context, id string, timeout time.Duration) (context.Context, context.CancelFunc, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.runningUpdates != nil && m.runningUpdates[id] {
		return nil, nil, false
	}
	if m.queuedUpdates[id] > 0 {
		m.queuedUpdates[id]--
	}
	if m.runningUpdates == nil {
		m.runningUpdates = make(map[string]bool)
	}
	m.runningUpdates[id] = true

	if m.updateCancels == nil {
		m.updateCancels = make(map[string]context.CancelFunc)
	}
	if m.updateWaiters == nil {
		m.updateWaiters = make(map[string]chan struct{})
	}

	updateCtx, cancel := context.WithTimeout(parentCtx, timeout)
	m.updateCancels[id] = cancel
	doneCh := make(chan struct{})
	m.updateWaiters[id] = doneCh

	return updateCtx, cancel, true
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

func (m *AppFeedManager) CancelFeedUpdate(id string) {
	m.mu.Lock()
	cancel, hasCancel := m.updateCancels[id]
	doneCh, hasDone := m.updateWaiters[id]
	if hasCancel && cancel != nil {
		log.WithField("feed_id", id).Info("canceling active feed update worker")
		cancel()
	}
	m.mu.Unlock()

	if hasDone && doneCh != nil {
		log.WithField("feed_id", id).Info("waiting for canceled feed update worker to stop...")
		select {
		case <-doneCh:
			log.WithField("feed_id", id).Info("feed update worker stopped successfully")
		case <-time.After(5 * time.Second):
			log.WithField("feed_id", id).Warn("timed out waiting for canceled update worker to stop")
		}
	}
}

func (m *AppFeedManager) MarkFeedUpdateFinished(id string) {
	m.mu.Lock()
	delete(m.runningUpdates, id)
	delete(m.updateCancels, id)
	if doneCh, ok := m.updateWaiters[id]; ok {
		close(doneCh)
		delete(m.updateWaiters, id)
	}
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

// SetPersonalFeeds attaches the separate personal-feed store for backup
// manifests and the optional human-readable safety snapshot.

// SetPersonalFeeds attaches the separate personal-feed store for backup
// manifests and the optional human-readable safety snapshot.
func (m *AppFeedManager) SetPersonalFeeds(service *personal.Service) {
	m.mu.Lock()
	m.personalFeeds = service
	m.mu.Unlock()
}

func (m *AppFeedManager) GetFeedConfig(id string) *feed.Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.feeds[id]
}
