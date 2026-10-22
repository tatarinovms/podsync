package personal

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mxpv/podsync/pkg/db"
	"github.com/mxpv/podsync/pkg/feed"
	"github.com/mxpv/podsync/pkg/fs"
	"github.com/mxpv/podsync/pkg/model"
	log "github.com/sirupsen/logrus"
)

type Processor interface {
	Process(context.Context, *Media) (io.ReadCloser, error)
}

type Service struct {
	mu        sync.Mutex
	store     db.PersonalStorage
	files     fs.Storage
	processor Processor
	host      string
	enqueue   func(context.Context, string) error
	busy      map[string]bool
	reserved  map[string]bool
}

func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
func validateMediaPath(name string) error {
	clean := filepath.Clean(filepath.FromSlash(name))
	relative, err := filepath.Rel("_personal", clean)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return invalid("media path must stay inside _personal")
	}
	return nil
}

func memberKey(feedID, episodeID string) string { return "episode/" + feedID + "/" + episodeID }
func invalid(message string) error              { return fmt.Errorf("%w: %s", ErrInvalid, message) }

// New initializes the default feed and marker atomically. Reserved IDs include
// configured channel feeds, including ones not yet written to the database.
func New(ctx context.Context, store db.PersonalStorage, files fs.Storage, processor Processor, host string, reserved map[string]bool, enqueue func(context.Context, string) error) (*Service, error) {
	s := &Service{store: store, files: files, processor: processor, host: strings.TrimRight(host, "/"), enqueue: enqueue, busy: map[string]bool{}, reserved: reserved}
	err := store.PersonalUpdate(func(tx db.PersonalTx) error {
		var done bool
		if err := tx.Get("initialized", &done); err == nil && done {
			return nil
		} else if err != nil && !errors.Is(err, model.ErrNotFound) {
			return err
		}
		if reserved["personal"] {
			return fmt.Errorf("%w: ID personal is already used by a channel feed; rename that feed before enabling personal feeds", ErrConflict)
		}
		var existing Feed
		if err := tx.Get("feed/personal", &existing); errors.Is(err, model.ErrNotFound) {
			now := time.Now().UTC()
			f := &Feed{ID: "personal", Type: "personal", Title: "My Feed", Format: model.FormatAudio, PlaylistSort: model.SortingDesc, CreatedAt: now, UpdatedAt: now}
			if err := tx.Put("feed/personal", f); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		return tx.Put("initialized", true)
	})
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.collectLocked(ctx); err != nil {
		return nil, err
	}
	feeds, err := s.listLocked()
	if err != nil {
		return nil, err
	}
	for _, f := range feeds {
		if reserved[f.ID] {
			return nil, fmt.Errorf("%w: duplicate feed ID %s", ErrConflict, f.ID)
		}
		if err := s.rebuildLocked(ctx, f.ID); err != nil {
			return nil, err
		}
		if enqueue != nil {
			if err := enqueue(ctx, f.ID); err != nil {
				return nil, err
			}
		}
	}
	return s, nil
}

func (s *Service) listLocked() ([]*Feed, error) {
	result := []*Feed{}
	err := s.store.PersonalView(func(tx db.PersonalTx) error {
		return tx.Walk("feed/", func(_ string, data []byte) error {
			var f Feed
			if err := json.Unmarshal(data, &f); err != nil {
				return err
			}
			f.FeedURL = s.host + "/" + f.ID + ".xml"
			result = append(result, &f)
			return nil
		})
	})
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.Before(result[j].CreatedAt) })
	return result, err
}
func (s *Service) List() ([]*Feed, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.listLocked() }
func (s *Service) Get(id string) (*Detail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.detailLocked(id)
}
func (s *Service) detailLocked(id string) (*Detail, error) {
	d := &Detail{Feed: &Feed{}, Episodes: []*model.Episode{}}
	err := s.store.PersonalView(func(tx db.PersonalTx) error {
		if err := tx.Get("feed/"+id, d.Feed); err != nil {
			return err
		}
		d.Feed.FeedURL = s.host + "/" + id + ".xml"
		return tx.Walk("episode/"+id+"/", func(_ string, data []byte) error {
			var member Membership
			if err := json.Unmarshal(data, &member); err != nil {
				return err
			}
			var media Media
			if err := tx.Get("media/"+member.MediaID, &media); err != nil {
				return err
			}
			mime := "audio/mpeg"
			if media.Format == model.FormatVideo {
				mime = "video/mp4"
			}
			ep := &model.Episode{ID: member.ID, MediaID: media.ID, Title: media.Title, Description: media.Description, Thumbnail: media.Thumbnail, Duration: media.Duration, Size: media.Size, VideoURL: media.SourceURL, PubDate: member.AddedAt, Status: media.Status, Error: media.Error, MediaType: mime}
			if media.Status == model.EpisodeDownloaded {
				ep.MediaURL = s.host + "/" + media.Path
			}
			d.Episodes = append(d.Episodes, ep)
			return nil
		})
	})
	sort.Slice(d.Episodes, func(i, j int) bool {
		if d.Episodes[i].PubDate.Equal(d.Episodes[j].PubDate) {
			return d.Episodes[i].ID < d.Episodes[j].ID
		}
		if d.Feed.PlaylistSort == model.SortingAsc {
			return d.Episodes[i].PubDate.Before(d.Episodes[j].PubDate)
		}
		return d.Episodes[i].PubDate.After(d.Episodes[j].PubDate)
	})
	return d, err
}
func apply(f *Feed, p Patch) error {
	if p.Title != nil {
		f.Title = strings.TrimSpace(*p.Title)
	}
	if f.Title == "" {
		return invalid("title is required")
	}
	if p.Description != nil {
		f.Description = *p.Description
	}
	if p.CoverArt != nil {
		f.CoverArt = strings.TrimSpace(*p.CoverArt)
		if f.CoverArt != "" {
			if err := validateURL(f.CoverArt); err != nil {
				return err
			}
		}
	}
	if p.Format != nil && *p.Format != "" {
		f.Format = *p.Format
	}
	if f.Format == "" {
		f.Format = "universal"
	}
	if p.PlaylistSort != nil {
		f.PlaylistSort = *p.PlaylistSort
	}
	if f.PlaylistSort != model.SortingAsc && f.PlaylistSort != model.SortingDesc {
		return invalid("playlist_sort must be asc or desc")
	}
	if p.PrivateFeed != nil {
		f.PrivateFeed = *p.PrivateFeed
	}
	f.UpdatedAt = time.Now().UTC()
	return nil
}
func validateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return invalid("an HTTP or HTTPS URL without embedded credentials is required")
	}
	return nil
}
func (s *Service) Create(ctx context.Context, p Patch, reserved map[string]bool) (*Feed, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := &Feed{Type: "personal", Format: "universal", PlaylistSort: model.SortingDesc, CreatedAt: time.Now().UTC()}
	if err := apply(f, p); err != nil {
		return nil, err
	}
	for {
		id, err := newID()
		if err != nil {
			return nil, err
		}
		f.ID = "personal-" + id
		if reserved[f.ID] || s.reserved[f.ID] {
			continue
		}
		err = s.store.PersonalUpdate(func(tx db.PersonalTx) error {
			var old Feed
			if err := tx.Get("feed/"+f.ID, &old); err == nil {
				return model.ErrAlreadyExists
			} else if !errors.Is(err, model.ErrNotFound) {
				return err
			}
			return tx.Put("feed/"+f.ID, f)
		})
		if errors.Is(err, model.ErrAlreadyExists) {
			continue
		}
		if err != nil {
			return nil, err
		}
		break
	}
	f.FeedURL = s.host + "/" + f.ID + ".xml"
	return f, s.rebuildLocked(ctx, f.ID)
}
func (s *Service) Patch(ctx context.Context, id string, p Patch) (*Feed, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var f Feed
	err := s.store.PersonalUpdate(func(tx db.PersonalTx) error {
		if err := tx.Get("feed/"+id, &f); err != nil {
			return err
		}
		if err := apply(&f, p); err != nil {
			return err
		}
		return tx.Put("feed/"+id, &f)
	})
	if err != nil {
		return nil, err
	}
	f.FeedURL = s.host + "/" + id + ".xml"
	return &f, s.rebuildLocked(ctx, id)
}
func (s *Service) rebuildLocked(ctx context.Context, id string) error {
	d, err := s.detailLocked(id)
	if err != nil {
		return err
	}
	f := d.Feed
	mf := &model.Feed{ID: id, Provider: model.ProviderPersonal, Title: f.Title, Description: f.Description, CoverArt: f.CoverArt, Format: f.Format, PrivateFeed: f.PrivateFeed, PlaylistSort: f.PlaylistSort, PubDate: f.CreatedAt, ItemURL: f.FeedURL, Episodes: d.Episodes}
	podcast, err := feed.Build(ctx, mf, f.Config(), s.host)
	if err != nil {
		return err
	}
	_, err = s.files.Create(ctx, id+".xml", strings.NewReader(feed.FormatXML(podcast, mf)))
	return err
}
func (s *Service) enqueueLocked(ctx context.Context, id string) error {
	if s.enqueue == nil {
		return nil
	}
	return s.enqueue(ctx, id)
}
func detectFormat(sourceURL, sourcePath, requestedFormat string) model.Format {
	req := strings.ToLower(strings.TrimSpace(requestedFormat))
	if req == "audio" {
		return model.FormatAudio
	}
	if req == "video" {
		return model.FormatVideo
	}
	target := sourcePath
	if target == "" {
		target = sourceURL
	}
	if u, err := url.Parse(target); err == nil && u.Path != "" {
		target = u.Path
	}
	ext := strings.ToLower(filepath.Ext(target))
	switch ext {
	case ".mp4", ".m4v", ".mov", ".webm", ".mkv":
		return model.FormatVideo
	default:
		return model.FormatAudio
	}
}

func (s *Service) AddURL(ctx context.Context, id, source, title, format string) (*model.Episode, error) {
	source = strings.TrimSpace(source)
	if err := validateURL(source); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addLocked(ctx, id, &Media{
		SourceURL: source,
		Title:     strings.TrimSpace(title),
		Format:    detectFormat(source, "", format),
	})
}
func (s *Service) addLocked(ctx context.Context, id string, media *Media) (*model.Episode, error) {
	mediaID, err := newID()
	if err != nil {
		return nil, err
	}
	memberID, err := newID()
	if err != nil {
		return nil, err
	}
	if media.ID == "" {
		media.ID = mediaID
	}
	if media.Format == "" {
		media.Format = detectFormat(media.SourceURL, media.SourcePath, "")
	}
	member := Membership{ID: memberID, MediaID: media.ID, AddedAt: time.Now().UTC()}
	err = s.store.PersonalUpdate(func(tx db.PersonalTx) error {
		var f Feed
		if err := tx.Get("feed/"+id, &f); err != nil {
			return err
		}
		media.References = 1
		media.Status = model.EpisodeNew
		ext := "mp3"
		if media.Format == model.FormatVideo {
			ext = "mp4"
		}
		media.Path = "_personal/" + media.ID + "/media." + ext
		if media.Title == "" {
			media.Title = media.SourceURL
		}
		if err := tx.Put("media/"+media.ID, media); err != nil {
			return err
		}
		if err := tx.Put(memberKey(id, member.ID), member); err != nil {
			return err
		}
		f.EpisodeCount++
		return tx.Put("feed/"+id, &f)
	})
	if err != nil {
		return nil, err
	}
	if err := s.enqueueLocked(ctx, id); err != nil {
		return nil, err
	}
	return &model.Episode{ID: member.ID, MediaID: media.ID, Title: media.Title, PubDate: member.AddedAt, Status: media.Status}, nil
}

// Upload uses only generated storage paths; filenames are never used as paths.
func (s *Service) Upload(ctx context.Context, id, filename string, r io.Reader, format string) (*model.Episode, error) {
	if filename != filepath.Base(filename) || strings.ContainsAny(filename, "/\\") || filename == "." || filename == ".." {
		return nil, invalid("invalid filename")
	}
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".mp3", ".m4a", ".m4b", ".mp4", ".m4v", ".mov", ".webm", ".mkv", ".wav", ".ogg", ".opus", ".flac", ".aac":
	default:
		return nil, invalid("unsupported media file")
	}
	mediaID, err := newID()
	if err != nil {
		return nil, err
	}
	source := "_personal/" + mediaID + "/source" + ext
	if err := validateMediaPath(source); err != nil {
		return nil, err
	}
	// Validate the feed before accepting a large upload, then again in the transaction.
	if _, err := s.Get(id); err != nil {
		return nil, err
	}
	if _, err := s.files.Create(ctx, source, r); err != nil {
		_ = s.files.Delete(context.Background(), source)
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	mediaFormat := detectFormat("", filename, format)
	ep, err := s.addLocked(ctx, id, &Media{
		ID:         mediaID,
		SourcePath: source,
		Title:      strings.TrimSuffix(filename, ext),
		Format:     mediaFormat,
	})
	if err != nil {
		// Queue failure must not delete a source already referenced by a persisted job.
		var m Media
		exists := s.store.PersonalView(func(tx db.PersonalTx) error { return tx.Get("media/"+mediaID, &m) }) == nil
		if !exists {
			_ = s.files.Delete(context.Background(), source)
		}
	}
	return ep, err
}

// AddPath allows importing a media file from a local server file path.
func (s *Service) AddPath(ctx context.Context, id, filePath, title, format string) (*model.Episode, error) {
	filePath = filepath.Clean(strings.TrimSpace(filePath))
	if filePath == "" || filePath == "." || filePath == "/" {
		return nil, invalid("file_path is required")
	}
	fi, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("file not found: %w", err)
	}
	if !fi.Mode().IsRegular() {
		return nil, invalid("target is not a regular file")
	}
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".mp3", ".m4a", ".m4b", ".mp4", ".m4v", ".mov", ".webm", ".mkv", ".wav", ".ogg", ".opus", ".flac", ".aac":
	default:
		return nil, invalid("unsupported media file format: " + ext)
	}
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	name := filepath.Base(filePath)
	if strings.TrimSpace(title) != "" {
		name = strings.TrimSpace(title) + ext
	}
	return s.Upload(ctx, id, name, f, format)
}

func (s *Service) Transfer(ctx context.Context, source, episodeID, target string, move bool) (*Membership, error) {
	if source == target {
		return nil, invalid("choose a different destination feed")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var member Membership
	copyID, err := newID()
	if err != nil {
		return nil, err
	}
	err = s.store.PersonalUpdate(func(tx db.PersonalTx) error {
		var src, dst Feed
		if err := tx.Get("feed/"+source, &src); err != nil {
			return err
		}
		if err := tx.Get("feed/"+target, &dst); err != nil {
			return err
		}
		if err := tx.Get(memberKey(source, episodeID), &member); err != nil {
			return err
		}
		var media Media
		if err := tx.Get("media/"+member.MediaID, &media); err != nil {
			return err
		}
		if move {
			if err := tx.Delete(memberKey(source, episodeID)); err != nil {
				return err
			}
			src.EpisodeCount--
			if err := tx.Put("feed/"+source, &src); err != nil {
				return err
			}
		} else {
			member.ID = copyID
			member.AddedAt = time.Now().UTC()
			media.References++
			if err := tx.Put("media/"+media.ID, &media); err != nil {
				return err
			}
		}
		if err := tx.Put(memberKey(target, member.ID), member); err != nil {
			return err
		}
		dst.EpisodeCount++
		return tx.Put("feed/"+target, &dst)
	})
	if err != nil {
		return nil, err
	}
	if move {
		if err := s.rebuildLocked(ctx, source); err != nil {
			return nil, err
		}
	}
	if err := s.rebuildLocked(ctx, target); err != nil {
		return nil, err
	}
	return &member, s.enqueueLocked(ctx, target)
}
func release(tx db.PersonalTx, member Membership, deleteFiles bool) error {
	var media Media
	if err := tx.Get("media/"+member.MediaID, &media); err != nil {
		return err
	}
	media.References--
	if media.References < 0 {
		return errors.New("negative media reference count")
	}
	if media.References == 0 {
		media.DeletePending = deleteFiles
	}
	return tx.Put("media/"+media.ID, media)
}
func (s *Service) DeleteEpisode(ctx context.Context, id, episodeID string, deleteFiles bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.store.PersonalUpdate(func(tx db.PersonalTx) error {
		var f Feed
		if err := tx.Get("feed/"+id, &f); err != nil {
			return err
		}
		var member Membership
		if err := tx.Get(memberKey(id, episodeID), &member); err != nil {
			return err
		}
		if err := release(tx, member, deleteFiles); err != nil {
			return err
		}
		if err := tx.Delete(memberKey(id, episodeID)); err != nil {
			return err
		}
		f.EpisodeCount--
		return tx.Put("feed/"+id, &f)
	})
	if err != nil {
		return err
	}
	if err := s.rebuildLocked(ctx, id); err != nil {
		return err
	}
	return s.collectLocked(ctx)
}
func (s *Service) Delete(ctx context.Context, id string, deleteFiles bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.store.PersonalUpdate(func(tx db.PersonalTx) error {
		var f Feed
		if err := tx.Get("feed/"+id, &f); err != nil {
			return err
		}
		if err := tx.Walk("episode/"+id+"/", func(key string, data []byte) error {
			var member Membership
			if err := json.Unmarshal(data, &member); err != nil {
				return err
			}
			if err := release(tx, member, deleteFiles); err != nil {
				return err
			}
			return tx.Delete(key)
		}); err != nil {
			return err
		}
		// Keep a tombstone until the public RSS file is removed, even across a crash.
		if err := tx.Put("deleted/"+id, true); err != nil {
			return err
		}
		return tx.Delete("feed/" + id)
	})
	if err != nil {
		return err
	}
	return s.collectLocked(ctx)
}
func (s *Service) collectLocked(ctx context.Context) error {
	var media []*Media
	var deleted []string
	err := s.store.PersonalView(func(tx db.PersonalTx) error {
		if err := tx.Walk("deleted/", func(key string, _ []byte) error {
			deleted = append(deleted, strings.TrimPrefix(key, "deleted/"))
			return nil
		}); err != nil {
			return err
		}
		return tx.Walk("media/", func(_ string, data []byte) error {
			var m Media
			if err := json.Unmarshal(data, &m); err != nil {
				return err
			}
			if m.References == 0 && m.DeletePending && !s.busy[m.ID] {
				media = append(media, &m)
			}
			return nil
		})
	})
	if err != nil {
		return err
	}
	for _, id := range deleted {
		if err := s.files.Delete(ctx, id+".xml"); err != nil && !errors.Is(err, os.ErrNotExist) && !os.IsNotExist(err) {
			log.WithError(err).Warnf("failed to delete feed xml %s", id)
		}
		if err := s.store.PersonalUpdate(func(tx db.PersonalTx) error { return tx.Delete("deleted/" + id) }); err != nil {
			return err
		}
	}
	for _, m := range media {
		for _, path := range []string{m.Path, m.SourcePath} {
			if path != "" {
				if err := validateMediaPath(path); err != nil {
					return err
				}
				if err := s.files.Delete(ctx, path); err != nil && !errors.Is(err, os.ErrNotExist) && !os.IsNotExist(err) {
					log.WithError(err).Warnf("failed to delete media file %s", path)
				}
			}
		}
		if err := s.store.PersonalUpdate(func(tx db.PersonalTx) error { return tx.Delete("media/" + m.ID) }); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) Retry(ctx context.Context, id, episodeID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.store.PersonalUpdate(func(tx db.PersonalTx) error {
		var member Membership
		if err := tx.Get(memberKey(id, episodeID), &member); err != nil {
			return err
		}
		var media Media
		if err := tx.Get("media/"+member.MediaID, &media); err != nil {
			return err
		}
		if media.Status != model.EpisodeDownloaded {
			media.Status = model.EpisodeNew
			media.Error = ""
		}
		return tx.Put("media/"+media.ID, media)
	})
	if err != nil {
		return err
	}
	return s.enqueueLocked(ctx, id)
}

// Update is run by Podsync's persistent queue. Downloads run outside the service
// lock, while mutations, reference checks and RSS publication are serialized.
func (s *Service) Update(ctx context.Context, id string) error {
	d, err := s.Get(id)
	if errors.Is(err, model.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	var failures []error
	for _, ep := range d.Episodes {
		if ep.Status == model.EpisodeDownloaded {
			continue
		}
		if err := s.process(ctx, ep.MediaID); err != nil {
			failures = append(failures, err)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	feeds, err := s.listLocked()
	if err != nil {
		return err
	}
	for _, f := range feeds {
		if err := s.rebuildLocked(ctx, f.ID); err != nil {
			failures = append(failures, err)
		}
	}
	if err := s.collectLocked(ctx); err != nil {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}
func (s *Service) process(ctx context.Context, id string) error {
	s.mu.Lock()
	var media Media
	err := s.store.PersonalView(func(tx db.PersonalTx) error { return tx.Get("media/"+id, &media) })
	if err != nil || media.References == 0 || media.Status == model.EpisodeDownloaded {
		s.mu.Unlock()
		return err
	}
	if s.busy[id] {
		s.mu.Unlock()
		return fmt.Errorf("media %s is already being processed", id)
	}
	if err := validateMediaPath(media.Path); err != nil {
		s.mu.Unlock()
		return err
	}
	s.busy[id] = true
	s.mu.Unlock()
	var size int64
	if s.processor == nil {
		err = errors.New("media processor unavailable")
	} else {
		var r io.ReadCloser
		r, err = s.processor.Process(ctx, &media)
		if err == nil {
			size, err = s.files.Create(ctx, media.Path, r)
			closeErr := r.Close()
			if err == nil {
				err = closeErr
			}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.busy, id)
	processErr := err
	err = s.store.PersonalUpdate(func(tx db.PersonalTx) error {
		var latest Media
		if err := tx.Get("media/"+id, &latest); err != nil {
			return err
		}
		latest.Title = media.Title
		latest.Description = media.Description
		latest.Thumbnail = media.Thumbnail
		latest.Duration = media.Duration
		latest.Path = media.Path
		if processErr != nil {
			latest.Status = model.EpisodeError
			latest.Error = processErr.Error()
		} else {
			latest.Status = model.EpisodeDownloaded
			latest.Error = ""
			latest.Size = size
		}
		return tx.Put("media/"+id, &latest)
	})
	if processErr != nil {
		_ = s.files.Delete(context.Background(), media.Path)
	}
	return errors.Join(processErr, err)
}
