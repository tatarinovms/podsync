// Package backup creates portable Podsync system backups. Archives contain
// configuration and database metadata only; downloaded media is never read.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/pelletier/go-toml"
)

const SchemaVersion = 1
const maxManifestSize = 1 << 20
const maxExtractedSize = int64(1 << 30)

type Counts struct {
	Feeds          int `json:"feeds"`
	PersonalFeeds  int `json:"personal_feeds"`
	AudiobookFeeds int `json:"audiobook_feeds"`
	CustomCovers   int `json:"custom_covers"`
}

type Manifest struct {
	SchemaVersion   int               `json:"schema_version"`
	PodsyncVersion  string            `json:"podsync_version"`
	CreatedAt       time.Time         `json:"created_at"`
	IncludesSecrets bool              `json:"includes_secrets"`
	Counts          Counts            `json:"counts"`
	Files           map[string]string `json:"files"`
}

type ExportOptions struct {
	ConfigPath       string
	CoversDir        string
	CookiesPath      string
	Version          string
	IncludeSecrets   bool
	Counts           Counts
	PersonalSnapshot []byte
	ExportDatabase   func(io.Writer) error
}

type RestoreOptions struct {
	ConfigPath            string
	CoversDir             string
	CookiesPath           string
	PreserveStoragePaths  bool
	RestoreSecrets        bool
	RestoreDatabase       bool
	RestoreCovers         bool
	RestoreCookies        bool
	RestoreDatabaseStream func(io.Reader) error
}

type RestoreResult struct {
	Manifest         *Manifest `json:"manifest"`
	ConfigRestored   bool      `json:"config_restored"`
	DatabaseRestored bool      `json:"database_restored"`
	CoversRestored   int       `json:"covers_restored"`
	CookiesRestored  bool      `json:"cookies_restored"`
}

func backupName(t time.Time) string {
	return "podsync-backup-" + t.UTC().Format("20060102-150405") + ".tar.gz"
}

// Filename returns the stable download name used by HTTP and CLI callers.
func Filename(now time.Time) string { return backupName(now) }

func Create(ctx context.Context, opts ExportOptions, w io.Writer) (*Manifest, error) {
	if opts.ExportDatabase == nil {
		return nil, fmt.Errorf("database export is not configured")
	}
	config, err := os.ReadFile(opts.ConfigPath)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	if !opts.IncludeSecrets {
		if config, err = withoutSecrets(config); err != nil {
			return nil, err
		}
	}
	dbFile, err := os.CreateTemp("", "podsync-badger-*.bak")
	if err != nil {
		return nil, err
	}
	dbPath := dbFile.Name()
	defer os.Remove(dbPath)
	if err := opts.ExportDatabase(dbFile); err != nil {
		_ = dbFile.Close()
		return nil, fmt.Errorf("export database: %w", err)
	}
	if err := dbFile.Close(); err != nil {
		return nil, err
	}
	manifest := &Manifest{SchemaVersion: SchemaVersion, PodsyncVersion: opts.Version, CreatedAt: time.Now().UTC(), IncludesSecrets: opts.IncludeSecrets, Counts: opts.Counts, Files: map[string]string{}}
	manifest.Files["config.toml"] = shaBytes(config)
	if manifest.Files["badger.bak"], err = shaFile(dbPath); err != nil {
		return nil, err
	}
	coverFiles, err := listCovers(opts.CoversDir)
	if err != nil {
		return nil, err
	}
	manifest.Counts.CustomCovers = len(coverFiles)
	for _, rel := range coverFiles {
		if manifest.Files["covers/"+filepath.ToSlash(rel)], err = shaFile(filepath.Join(opts.CoversDir, rel)); err != nil {
			return nil, err
		}
	}
	if len(opts.PersonalSnapshot) > 0 {
		manifest.Files["personal_feeds.json"] = shaBytes(opts.PersonalSnapshot)
	}
	if opts.IncludeSecrets && opts.CookiesPath != "" {
		if _, err := os.Stat(opts.CookiesPath); err == nil {
			if manifest.Files["cookies.txt"], err = shaFile(opts.CookiesPath); err != nil {
				return nil, err
			}
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	defer tw.Close()
	defer gz.Close()
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := writeBytes(tw, "manifest.json", manifestJSON, manifest.CreatedAt); err != nil {
		return nil, err
	}
	if err := writeBytes(tw, "config.toml", config, manifest.CreatedAt); err != nil {
		return nil, err
	}
	if err := writeFile(tw, "badger.bak", dbPath, manifest.CreatedAt); err != nil {
		return nil, err
	}
	for _, rel := range coverFiles {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := writeFile(tw, "covers/"+filepath.ToSlash(rel), filepath.Join(opts.CoversDir, rel), manifest.CreatedAt); err != nil {
			return nil, err
		}
	}
	if len(opts.PersonalSnapshot) > 0 {
		if err := writeBytes(tw, "personal_feeds.json", opts.PersonalSnapshot, manifest.CreatedAt); err != nil {
			return nil, err
		}
	}
	if _, ok := manifest.Files["cookies.txt"]; ok {
		if err := writeFile(tw, "cookies.txt", opts.CookiesPath, manifest.CreatedAt); err != nil {
			return nil, err
		}
	}
	return manifest, nil
}

func Inspect(r io.Reader) (*Manifest, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("open gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	hashes := map[string]string{}
	var manifest *Manifest
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read archive: %w", err)
		}
		if err := validName(h.Name); err != nil {
			return nil, err
		}
		if h.Name == "manifest.json" {
			if h.Size > maxManifestSize {
				return nil, fmt.Errorf("manifest.json is too large")
			}
			data, err := io.ReadAll(io.LimitReader(tr, maxManifestSize+1))
			if err != nil {
				return nil, err
			}
			var m Manifest
			if err := json.Unmarshal(data, &m); err != nil {
				return nil, fmt.Errorf("invalid manifest: %w", err)
			}
			manifest = &m
		} else {
			hash := sha256.New()
			if _, err := io.Copy(hash, tr); err != nil {
				return nil, err
			}
			hashes[h.Name] = hex.EncodeToString(hash.Sum(nil))
		}
	}
	if manifest == nil {
		return nil, fmt.Errorf("backup has no manifest.json")
	}
	if manifest.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("unsupported backup schema version %d", manifest.SchemaVersion)
	}
	for name, expected := range manifest.Files {
		if got := hashes[name]; got == "" || got != expected {
			return nil, fmt.Errorf("backup checksum mismatch for %s", name)
		}
	}
	if _, ok := manifest.Files["config.toml"]; !ok {
		return nil, fmt.Errorf("backup does not contain config.toml")
	}
	if _, ok := manifest.Files["badger.bak"]; !ok {
		return nil, fmt.Errorf("backup does not contain badger.bak")
	}
	return manifest, nil
}

// Restore verifies every entry before changing files. The database callback is
// intentionally last because its caller owns runtime write quiescing.
func Restore(ctx context.Context, r io.Reader, opts RestoreOptions) (*RestoreResult, error) {
	tmp, err := os.MkdirTemp("", "podsync-restore-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	if err := extract(r, tmp); err != nil {
		return nil, err
	}
	f, err := os.Open(filepath.Join(tmp, "archive.tar.gz"))
	if err != nil {
		return nil, err
	}
	manifest, inspectErr := Inspect(f)
	_ = f.Close()
	if inspectErr != nil {
		return nil, inspectErr
	}
	result := &RestoreResult{Manifest: manifest}
	config, err := os.ReadFile(filepath.Join(tmp, "config.toml"))
	if err != nil {
		return nil, err
	}
	if opts.PreserveStoragePaths || !opts.RestoreSecrets {
		current, readErr := os.ReadFile(opts.ConfigPath)
		if readErr == nil {
			config, err = mergeConfig(config, current, opts.PreserveStoragePaths, !opts.RestoreSecrets)
			if err != nil {
				return nil, err
			}
		}
	}
	if err := writeAtomic(opts.ConfigPath, config); err != nil {
		return nil, err
	}
	result.ConfigRestored = true
	if opts.RestoreCovers {
		n, err := copyCovers(filepath.Join(tmp, "covers"), opts.CoversDir)
		if err != nil {
			return nil, err
		}
		result.CoversRestored = n
	}
	if opts.RestoreCookies && manifest.IncludesSecrets {
		if b, err := os.ReadFile(filepath.Join(tmp, "cookies.txt")); err == nil {
			if err := writeAtomic(opts.CookiesPath, b); err != nil {
				return nil, err
			}
			result.CookiesRestored = true
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}
	if opts.RestoreDatabase {
		if opts.RestoreDatabaseStream == nil {
			return nil, fmt.Errorf("database restore is not configured")
		}
		f, err := os.Open(filepath.Join(tmp, "badger.bak"))
		if err != nil {
			return nil, err
		}
		err = opts.RestoreDatabaseStream(f)
		_ = f.Close()
		if err != nil {
			return nil, fmt.Errorf("restore database: %w", err)
		}
		result.DatabaseRestored = true
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func extract(r io.Reader, dir string) error {
	archive := filepath.Join(dir, "archive.tar.gz")
	out, err := os.Create(archive)
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, r); err != nil {
		_ = out.Close()
		return err
	}
	if err = out.Close(); err != nil {
		return err
	}
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var extracted int64
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if err := validName(h.Name); err != nil {
			return err
		}
		if h.Size < 0 || h.Size > maxExtractedSize || extracted+h.Size > maxExtractedSize {
			return fmt.Errorf("backup contents exceed the 1 GiB extraction limit")
		}
		target := filepath.Join(dir, filepath.FromSlash(h.Name))
		if h.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		o, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, err = io.Copy(o, tr)
		closeErr := o.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		extracted += h.Size
	}
}

func validName(name string) error {
	clean := filepath.ToSlash(filepath.Clean(name))
	if clean == "." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") || strings.Contains(clean, ":") {
		return fmt.Errorf("unsafe archive path %q", name)
	}
	return nil
}
func shaBytes(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func shaFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	_, err = io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), err
}
func writeBytes(tw *tar.Writer, name string, data []byte, t time.Time) error {
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(data)), ModTime: t}); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
}
func writeFile(tw *tar.Writer, name, path string, t time.Time) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if err = tw.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: info.Size(), ModTime: t}); err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(tw, f)
	return err
}
func listCovers(root string) ([]string, error) {
	if root == "" {
		return nil, nil
	}
	var out []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".webp" {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || strings.HasPrefix(rel, "..") {
			return fmt.Errorf("unsafe cover path %q", path)
		}
		out = append(out, rel)
		return nil
	})
	if os.IsNotExist(err) {
		return nil, nil
	}
	sort.Strings(out)
	return out, err
}
func copyCovers(src, dst string) (int, error) {
	files, err := listCovers(src)
	if err != nil {
		return 0, err
	}
	for _, rel := range files {
		b, err := os.ReadFile(filepath.Join(src, rel))
		if err != nil {
			return 0, err
		}
		if err := writeAtomic(filepath.Join(dst, rel), b); err != nil {
			return 0, err
		}
	}
	return len(files), nil
}
func writeAtomic(path string, b []byte) error {
	if path == "" {
		return fmt.Errorf("backup path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".podsync-backup-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(b); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Chmod(0600); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
func withoutSecrets(data []byte) ([]byte, error) {
	tree, err := toml.LoadBytes(data)
	if err != nil {
		return nil, err
	}
	for _, key := range []string{"tokens", "server.admin.password", "downloader.proxy", "downloader.proxies"} {
		tree.Delete(key)
	}
	return []byte(tree.String()), nil
}
func mergeConfig(incoming, current []byte, preserveStoragePaths, preserveSecrets bool) ([]byte, error) {
	in, err := toml.LoadBytes(incoming)
	if err != nil {
		return nil, err
	}
	cur, err := toml.LoadBytes(current)
	if err != nil {
		return nil, err
	}
	paths := []string{}
	if preserveStoragePaths {
		paths = []string{"storage", "database", "audiobooks.dir", "server.data_dir", "server.port"}
	}
	for _, key := range paths {
		if value := cur.Get(key); value != nil {
			in.Set(key, value)
		}
	}
	if preserveSecrets {
		for _, key := range []string{"tokens", "server.admin.password", "downloader.proxy", "downloader.proxies"} {
			if value := cur.Get(key); value != nil {
				in.Set(key, value)
			}
		}
	}
	return []byte(in.String()), nil
}
