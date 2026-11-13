package main

// Local audiobook discovery, import, file management and covers.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/mxpv/podsync/pkg/builder"
	"github.com/mxpv/podsync/pkg/feed"
	"github.com/mxpv/podsync/pkg/fs"
	"github.com/mxpv/podsync/pkg/model"
	"github.com/mxpv/podsync/services/web"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

// nonSlugCharactersPattern normalizes generated audiobook slugs.
var nonSlugCharactersPattern = regexp.MustCompile(`[^a-z0-9_-]+`)

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
	reg := nonSlugCharactersPattern
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
	if !validFeedIDPattern.MatchString(id) {
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
