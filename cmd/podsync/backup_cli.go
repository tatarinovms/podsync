package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mxpv/podsync/pkg/backup"
	"github.com/mxpv/podsync/pkg/db"
)

func backupLocations(cfg *Config) (covers, cookies string) {
	if cfg == nil || cfg.Storage.Type != "local" {
		return "", ""
	}
	// Custom covers can live in data/covers or beside a feed as cover.jpg.
	covers = cfg.Storage.Local.DataDir
	cookies = cfg.Downloader.CookiesFile
	if cookies == "" {
		cookies = filepath.Join(cfg.Storage.Local.DataDir, "cookies.txt")
	}
	return covers, cookies
}

func runBackupCLI(ctx context.Context, opts Opts, cfg *Config, database *db.Badger, version string) error {
	covers, cookies := backupLocations(cfg)
	if opts.ExportBackup != "" {
		out, err := os.OpenFile(opts.ExportBackup, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		counts := backup.Counts{}
		for _, f := range cfg.Feeds {
			if strings.HasPrefix(f.URL, "audiobook://") {
				counts.AudiobookFeeds++
			} else {
				counts.Feeds++
			}
		}
		manifest, err := backup.Create(ctx, backup.ExportOptions{ConfigPath: opts.ConfigPath, CoversDir: covers, CookiesPath: cookies, Version: version, IncludeSecrets: true, Counts: counts, ExportDatabase: database.ExportBackup}, out)
		closeErr := out.Close()
		if err != nil {
			_ = os.Remove(opts.ExportBackup)
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		fmt.Printf("Created %s (%d feeds, %d covers)\n", opts.ExportBackup, manifest.Counts.Feeds+manifest.Counts.AudiobookFeeds, manifest.Counts.CustomCovers)
		return nil
	}
	in, err := os.Open(opts.RestoreBackup)
	if err != nil {
		return err
	}
	defer in.Close()
	// Cross-server restores keep the target's mount paths and port unless the
	// caller explicitly requests a complete configuration replacement.
	preservePaths := !opts.NoPreservePaths
	result, err := backup.Restore(ctx, in, backup.RestoreOptions{ConfigPath: opts.ConfigPath, CoversDir: covers, CookiesPath: cookies, PreserveStoragePaths: preservePaths, RestoreSecrets: true, RestoreDatabase: true, RestoreCovers: true, RestoreCookies: true, RestoreDatabaseStream: database.RestoreBackup})
	if err != nil {
		return err
	}
	fmt.Printf("Restored backup created at %s; restart Podsync to rebuild feeds.\n", result.Manifest.CreatedAt.Format("2006-01-02 15:04:05 UTC"))
	return nil
}
