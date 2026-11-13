package main

// Portable system backup and restore.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/mxpv/podsync/pkg/backup"
	"github.com/mxpv/podsync/pkg/db"
	"github.com/mxpv/podsync/services/web"
	"github.com/pkg/errors"
)

func backupInfo(manifest *backup.Manifest) *web.BackupInfo {
	info := &web.BackupInfo{SchemaVersion: manifest.SchemaVersion, PodsyncVersion: manifest.PodsyncVersion, CreatedAt: manifest.CreatedAt, IncludesSecrets: manifest.IncludesSecrets}
	info.Counts.Feeds = manifest.Counts.Feeds
	info.Counts.PersonalFeeds = manifest.Counts.PersonalFeeds
	info.Counts.AudiobookFeeds = manifest.Counts.AudiobookFeeds
	info.Counts.CustomCovers = manifest.Counts.CustomCovers
	return info
}

func (m *AppFeedManager) backupLocationsLocked() (configPath, coversDir, cookiesPath string) {
	configPath = m.configPath
	if m.cfg == nil {
		return
	}
	if m.cfg.Storage.Type == "local" {
		// Covers may be stored in data/covers or alongside a regular or
		// audiobook feed as cover.jpg. The backup walker includes image files
		// only, so it never includes downloaded episode media.
		coversDir = m.cfg.Storage.Local.DataDir
		cookiesPath = m.cfg.Downloader.CookiesFile
		if cookiesPath == "" {
			cookiesPath = filepath.Join(m.cfg.Storage.Local.DataDir, "cookies.txt")
		}
	}
	return
}

// CreateSystemBackup streams a portable configuration/database archive. Media
// directories are never traversed; only the dedicated custom-covers directory
// is included.

// CreateSystemBackup streams a portable configuration/database archive. Media
// directories are never traversed; only the dedicated custom-covers directory
// is included.
func (m *AppFeedManager) CreateSystemBackup(ctx context.Context, includeSecrets bool, w io.Writer) (*web.BackupInfo, error) {
	store, ok := m.db.(db.BackupStorage)
	if !ok {
		return nil, errors.New("configured database does not support backups")
	}
	m.mu.RLock()
	configPath, coversDir, cookiesPath := m.backupLocationsLocked()
	counts := backup.Counts{}
	for _, f := range m.feeds {
		if strings.HasPrefix(f.URL, "audiobook://") {
			counts.AudiobookFeeds++
		} else {
			counts.Feeds++
		}
	}
	version := m.version
	personalFeeds := m.personalFeeds
	m.mu.RUnlock()
	var personalSnapshot []byte
	if personalFeeds != nil {
		if feeds, err := personalFeeds.List(); err == nil {
			counts.PersonalFeeds = len(feeds)
			personalSnapshot, _ = json.MarshalIndent(feeds, "", "  ")
		}
	}
	manifest, err := backup.Create(ctx, backup.ExportOptions{ConfigPath: configPath, CoversDir: coversDir, CookiesPath: cookiesPath, Version: version, IncludeSecrets: includeSecrets, Counts: counts, PersonalSnapshot: personalSnapshot, ExportDatabase: store.ExportBackup}, w)
	if err != nil {
		return nil, err
	}
	return backupInfo(manifest), nil
}

func (m *AppFeedManager) InspectSystemBackup(_ context.Context, r io.Reader) (*web.BackupInfo, error) {
	manifest, err := backup.Inspect(r)
	if err != nil {
		return nil, err
	}
	return backupInfo(manifest), nil
}

// RestoreSystemBackup pauses cron dispatch and refuses to overwrite a database
// while a queue worker owns a job. This avoids interleaving a native Badger
// Load with download-state writes. Queued work is part of the restored dump.

// RestoreSystemBackup pauses cron dispatch and refuses to overwrite a database
// while a queue worker owns a job. This avoids interleaving a native Badger
// Load with download-state writes. Queued work is part of the restored dump.
func (m *AppFeedManager) RestoreSystemBackup(ctx context.Context, r io.Reader, opts web.BackupRestoreOptions) (*web.BackupInfo, error) {
	store, ok := m.db.(db.BackupStorage)
	if !ok {
		return nil, errors.New("configured database does not support backups")
	}
	if m.queue != nil {
		if active, err := m.queue.GetActiveJobsMap(ctx); err == nil && len(active) > 0 {
			return nil, errors.New("wait for active downloads to finish before restoring a backup")
		}
	}
	m.configWriteMu.Lock()
	defer m.configWriteMu.Unlock()
	if m.cron != nil {
		stop := m.cron.Stop()
		select {
		case <-stop.Done():
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		defer m.cron.Start()
	}
	m.mu.RLock()
	configPath, coversDir, cookiesPath := m.backupLocationsLocked()
	m.mu.RUnlock()
	result, err := backup.Restore(ctx, r, backup.RestoreOptions{ConfigPath: configPath, CoversDir: coversDir, CookiesPath: cookiesPath, PreserveStoragePaths: opts.PreservePaths, RestoreSecrets: opts.RestoreSecrets, RestoreDatabase: true, RestoreCovers: true, RestoreCookies: opts.RestoreSecrets, RestoreDatabaseStream: store.RestoreBackup})
	if err != nil {
		return nil, err
	}
	// Re-read the persisted configuration. Keep the original map allocation so
	// the updater and cron closures see restored feed definitions immediately.
	if restored, err := LoadConfig(configPath); err == nil {
		m.mu.Lock()
		m.cfg = restored
		for id := range m.feeds {
			delete(m.feeds, id)
		}
		for id, f := range restored.Feeds {
			m.feeds[id] = f
		}
		for provider := range m.tokens {
			delete(m.tokens, provider)
		}
		for provider, tokens := range restored.Tokens {
			m.tokens[provider] = tokens
		}
		m.mu.Unlock()
		for _, f := range restored.Feeds {
			select {
			case m.updates <- f:
			default:
			}
		}
	} else {
		return nil, fmt.Errorf("restored backup but could not reload config: %w", err)
	}
	return backupInfo(result.Manifest), nil
}
