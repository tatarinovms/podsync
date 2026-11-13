package main

// Feed lifecycle: creation, configuration updates, deletion and update admission.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mxpv/podsync/pkg/builder"
	"github.com/mxpv/podsync/pkg/feed"
	"github.com/mxpv/podsync/pkg/fs"
	"github.com/mxpv/podsync/pkg/model"
	"github.com/mxpv/podsync/services/web"
	"github.com/pelletier/go-toml"
	"github.com/pkg/errors"
	"github.com/robfig/cron/v3"
	log "github.com/sirupsen/logrus"
)

func (m *AppFeedManager) AddFeed(ctx context.Context, newFeed *feed.Config) error {
	m.configWriteMu.Lock()
	defer m.configWriteMu.Unlock()

	if !validFeedIDPattern.MatchString(newFeed.ID) {
		return errors.New("feed ID must contain only Latin letters, digits, underscores, or hyphens")
	}

	if newFeed.ID == "personal" || strings.HasPrefix(newFeed.ID, "personal-") || newFeed.ID == "_personal" {
		return errors.New("this feed ID is reserved for personal feeds")
	}

	m.mu.RLock()
	if _, exists := m.feeds[newFeed.ID]; exists {
		m.mu.RUnlock()
		return errors.Errorf("feed with ID %q already exists", newFeed.ID)
	}
	if err := m.checkUpdateCapacityLocked(); err != nil {
		m.mu.RUnlock()
		return err
	}
	m.mu.RUnlock()

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

	if err := m.saveFeedToConfig(newFeed); err != nil {
		return errors.Wrap(err, "failed to save feed configuration")
	}

	m.mu.Lock()
	if _, exists := m.feeds[newFeed.ID]; exists {
		m.mu.Unlock()
		return errors.Errorf("feed with ID %q already exists", newFeed.ID)
	}

	// Add to in-memory map
	m.feeds[newFeed.ID] = newFeed
	if m.updater != nil {
		m.updater.SetFeedConfig(newFeed.ID, newFeed)
	}
	if m.cfg != nil && m.cfg.Feeds != nil {
		m.cfg.Feeds[newFeed.ID] = newFeed
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

	queueErr := m.queueUpdateLocked(newFeed)
	m.mu.Unlock()
	return queueErr
}

func (m *AppFeedManager) DeleteFeed(ctx context.Context, id string, deleteFiles bool) error {
	// 1. Immediately cancel active update worker and wait for it to stop cleanly
	m.CancelFeedUpdate(id)

	// 2. Cancel and remove any jobs in job queue
	if m.queue != nil {
		_ = m.queue.CancelFeed(ctx, id)
	}

	m.configWriteMu.Lock()
	defer m.configWriteMu.Unlock()

	m.mu.RLock()
	f, exists := m.feeds[id]
	if !exists {
		m.mu.RUnlock()
		return errors.Errorf("feed %q not found", id)
	}
	feedURL := f.URL
	var dataDir, storageType string
	if m.cfg != nil {
		dataDir = m.cfg.Storage.Local.DataDir
		storageType = m.cfg.Storage.Type
	}
	m.mu.RUnlock()

	if err := m.deleteFeedFromConfig(id); err != nil {
		return errors.Wrapf(err, "failed to remove feed %q from configuration", id)
	}

	m.mu.Lock()
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
	delete(m.queuedUpdates, id)
	delete(m.runningUpdates, id)
	delete(m.updateCancels, id)
	if doneCh, ok := m.updateWaiters[id]; ok {
		close(doneCh)
		delete(m.updateWaiters, id)
	}
	if m.cfg != nil {
		delete(m.cfg.Feeds, id)
	}
	if m.updater != nil {
		m.updater.SetFeedConfig(id, nil)
	}
	m.coverCache.Delete(id)
	m.mu.Unlock()

	// Clean database
	if err := m.db.DeleteFeed(ctx, id); err != nil {
		log.WithError(err).Warnf("failed to delete feed %q from database", id)
	}

	// Optionally delete files from disk/storage
	if deleteFiles {
		_ = m.storage.Delete(ctx, fmt.Sprintf("%s.xml", id))
		info, err := builder.ParseURL(feedURL)
		isAudiobook := (err == nil && info.Provider == model.ProviderAudiobook)
		if storageType == "local" && !isAudiobook {
			feedDir := filepath.Join(dataDir, id)
			_ = os.RemoveAll(feedDir)
			log.Infof("deleted files for feed %q at %s", id, feedDir)
		}
	}

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

func (m *AppFeedManager) UpdateFeed(ctx context.Context, id string, update *web.FeedConfigUpdate) error {
	m.configWriteMu.Lock()
	defer m.configWriteMu.Unlock()

	m.mu.RLock()
	f, exists := m.feeds[id]
	if !exists {
		m.mu.RUnlock()
		return errors.Errorf("feed %q not found", id)
	}
	resolved, err := update.WithCurrent(f)
	if err != nil {
		m.mu.RUnlock()
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
	if update.IgnoreLive != nil {
		f.IgnoreLive = *update.IgnoreLive
	}
	if update.NormalizeAudio != nil {
		f.NormalizeAudio = *update.NormalizeAudio
	}
	if update.Proxy != nil {
		f.Proxy = strings.TrimSpace(*update.Proxy)
	}

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
			m.mu.RUnlock()
			return errors.Wrap(err, "invalid update schedule")
		}
	}
	if err := m.checkUpdateCapacityLocked(); err != nil {
		m.mu.RUnlock()
		return err
	}

	oldKeepLast := 0
	if oldFeed.Clean != nil {
		oldKeepLast = oldFeed.Clean.KeepLast
	}
	newKeepLast := 0
	if f.Clean != nil {
		newKeepLast = f.Clean.KeepLast
	}
	limitsChanged := (oldFeed.PageSize != f.PageSize) || (oldKeepLast != newKeepLast)

	m.mu.RUnlock()

	// If limits or redownload changed while an update is actively downloading,
	// cancel the in-flight worker immediately so it stops downloading unwanted episodes.
	if limitsChanged || shouldRedownload {
		m.mu.RLock()
		isUpdating := m.runningUpdates != nil && m.runningUpdates[id]
		m.mu.RUnlock()
		if isUpdating {
			log.WithField("feed_id", id).Info("feed settings changed during active update; canceling in-flight download")
			m.CancelFeedUpdate(id)
		}
	}

	// Persist config to disk without holding m.mu
	if err := m.saveFeedToConfig(f); err != nil {
		return err
	}

	m.mu.Lock()
	if _, exists := m.feeds[id]; !exists {
		m.mu.Unlock()
		return errors.Errorf("feed %q not found", id)
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
	if f.IgnoreLive {
		tree.Set(prefix+".ignore_live", true)
	} else {
		tree.Delete(prefix + ".ignore_live")
	}
	if f.NormalizeAudio {
		tree.Set(prefix+".normalize_audio", true)
	} else {
		tree.Delete(prefix + ".normalize_audio")
	}
	if f.Proxy != "" {
		tree.Set(prefix+".proxy", f.Proxy)
	} else {
		tree.Delete(prefix + ".proxy")
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
