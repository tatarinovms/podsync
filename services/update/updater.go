package update

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/mxpv/podsync/pkg/builder"
	"github.com/mxpv/podsync/pkg/db"
	"github.com/mxpv/podsync/pkg/feed"
	"github.com/mxpv/podsync/pkg/fs"
	"github.com/mxpv/podsync/pkg/model"
	"github.com/mxpv/podsync/pkg/ytdl"
)

type Downloader interface {
	Download(ctx context.Context, feedConfig *feed.Config, episode *model.Episode) (io.ReadCloser, error)
	PlaylistMetadata(ctx context.Context, url string) (metadata ytdl.PlaylistMetadata, err error)
}

type TokenList []string

type Manager struct {
	feedsMu    sync.RWMutex
	hostname   string
	downloader Downloader
	db         db.Storage
	fs         fs.Storage
	feeds      map[string]*feed.Config
	keys       map[model.Provider]feed.KeyProvider
}

func NewUpdater(
	feeds map[string]*feed.Config,
	keys map[model.Provider]feed.KeyProvider,
	hostname string,
	downloader Downloader,
	db db.Storage,
	fs fs.Storage,
) (*Manager, error) {
	feedSnapshot := make(map[string]*feed.Config, len(feeds))
	for id, cfg := range feeds {
		feedSnapshot[id] = cfg
	}
	return &Manager{
		hostname:   hostname,
		downloader: downloader,
		db:         db,
		fs:         fs,
		feeds:      feedSnapshot,
		keys:       keys,
	}, nil
}

func (u *Manager) SetFeedConfig(id string, cfg *feed.Config) {
	u.feedsMu.Lock()
	defer u.feedsMu.Unlock()
	if cfg == nil {
		delete(u.feeds, id)
	} else {
		u.feeds[id] = cfg
	}
}

func (u *Manager) Update(ctx context.Context, feedConfig *feed.Config) error {
	log.WithFields(log.Fields{
		"feed_id": feedConfig.ID,
		"format":  feedConfig.Format,
		"quality": feedConfig.Quality,
	}).Infof("-> updating %s", feedConfig.URL)

	started := time.Now()

	if err := u.updateFeed(ctx, feedConfig); err != nil {
		return errors.Wrap(err, "update failed")
	}

	// Rebuild initial XML and OPML immediately so the feed is accessible to clients right away
	if xmlErr := u.buildXML(ctx, feedConfig); xmlErr != nil {
		log.WithError(xmlErr).Warn("initial xml build failed")
	}
	if opmlErr := u.buildOPML(ctx); opmlErr != nil {
		log.WithError(opmlErr).Warn("initial opml build failed")
	}

	// Fetch episodes for download
	episodesToDownload, err := u.fetchEpisodes(ctx, feedConfig)
	if err != nil {
		return errors.Wrap(err, "fetch episodes failed")
	}

	if err := u.downloadEpisodes(ctx, feedConfig, episodesToDownload); err != nil {
		return errors.Wrap(err, "download failed")
	}

	if err := u.cleanup(ctx, feedConfig); err != nil {
		log.WithError(err).Error("cleanup failed")
	}

	if err := u.buildXML(ctx, feedConfig); err != nil {
		return errors.Wrap(err, "xml build failed")
	}

	if err := u.buildOPML(ctx); err != nil {
		return errors.Wrap(err, "opml build failed")
	}

	elapsed := time.Since(started)
	log.Infof("successfully updated feed in %s", elapsed)
	return nil
}

// updateFeed pulls API for new episodes and saves them to database
func (u *Manager) updateFeed(ctx context.Context, feedConfig *feed.Config) error {
	info, err := builder.ParseURL(feedConfig.URL)
	if err != nil {
		return errors.Wrapf(err, "failed to parse URL: %s", feedConfig.URL)
	}

	var key string
	if keyProvider, ok := u.keys[info.Provider]; ok {
		key = keyProvider.Get()
	} else if info.Provider != model.ProviderRutube && info.Provider != model.ProviderSoundcloud && info.Provider != model.ProviderAudiobook {
		return errors.Errorf("key provider %q not loaded", info.Provider)
	}

	// Create an updater for this feed type
	provider, err := builder.New(ctx, info.Provider, key, u.downloader)
	if err != nil {
		return err
	}

	// Query API to get episodes
	log.Debug("building feed")
	result, err := provider.Build(ctx, feedConfig)
	if info.Provider == model.ProviderAudiobook && errors.Is(err, builder.ErrEmptyAudiobook) {
		// An emptied existing book must not keep broken chapter links in RSS.
		existing, readErr := u.db.GetFeed(ctx, feedConfig.ID)
		if readErr != nil {
			return err
		}
		existing.Episodes = make([]*model.Episode, 0)
		existing.UpdatedAt = time.Now().UTC()
		return u.db.AddFeed(ctx, feedConfig.ID, existing)
	}
	if err != nil {
		return err
	}

	log.Debugf("received %d episode(s) for %q", len(result.Episodes), result.Title)

	episodeSet := make(map[string]struct{})
	if err := u.db.WalkEpisodes(ctx, feedConfig.ID, func(episode *model.Episode) error {
		if episode.Status != model.EpisodeDownloaded && episode.Status != model.EpisodeCleaned {
			episodeSet[episode.ID] = struct{}{}
		}
		return nil
	}); err != nil {
		return err
	}

	if err := u.db.AddFeed(ctx, feedConfig.ID, result); err != nil {
		return err
	}

	for _, episode := range result.Episodes {
		delete(episodeSet, episode.ID)
	}

	// removing episodes that are no longer available in the feed and not downloaded or cleaned
	for id := range episodeSet {
		log.Infof("removing episode %q", id)
		err := u.db.DeleteEpisode(feedConfig.ID, id)
		if err != nil {
			return err
		}
	}

	log.Debug("successfully saved updates to storage")
	return nil
}

func (u *Manager) fetchEpisodes(ctx context.Context, feedConfig *feed.Config) ([]*model.Episode, error) {
	var (
		feedID   = feedConfig.ID
		pageSize = feedConfig.PageSize
	)

	log.WithField("page_size", pageSize).Info("fetching episodes for download")

	var candidates []*model.Episode
	err := u.db.WalkEpisodes(ctx, feedID, func(episode *model.Episode) error {
		var (
			logger = log.WithFields(log.Fields{"episode_id": episode.ID})
		)
		if episode.Status != model.EpisodeNew && episode.Status != model.EpisodeError {
			// File already downloaded
			logger.Infof("skipping due to already downloaded")
			return nil
		}

		if !matchFilters(episode, &feedConfig.Filters) {
			return nil
		}

		candidates = append(candidates, episode)
		return nil
	})

	if err != nil {
		return nil, errors.Wrapf(err, "failed to build update list")
	}

	// Sort candidates so the newest episodes are prioritized (or oldest first for chronological ascending playlists)
	if feedConfig.PlaylistSort == model.SortingAsc {
		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].PubDate.Before(candidates[j].PubDate)
		})
	} else {
		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].PubDate.After(candidates[j].PubDate)
		})
	}

	if pageSize > 0 && len(candidates) > pageSize {
		candidates = candidates[:pageSize]
	}

	for _, episode := range candidates {
		log.Debugf("adding %s (%q) to queue", episode.ID, episode.Title)
	}

	return candidates, nil
}

func (u *Manager) downloadEpisodes(ctx context.Context, feedConfig *feed.Config, downloadList []*model.Episode) error {
	var (
		downloadCount = len(downloadList)
		downloaded    = 0
		feedID        = feedConfig.ID
	)

	if downloadCount > 0 {
		log.Infof("download count: %d", downloadCount)
	} else {
		log.Info("no episodes to download")
		return nil
	}

	// Download pending episodes

	for idx, episode := range downloadList {
		var (
			logger      = log.WithFields(log.Fields{"feed_id": feedID, "index": idx, "episode_id": episode.ID})
			episodeName = feed.EpisodeName(feedConfig, episode)
		)

		// Check whether episode already exists
		size, err := u.fs.Size(ctx, fmt.Sprintf("%s/%s", feedID, episodeName))
		if err == nil {
			logger.Infof("episode %q already exists on disk", episode.ID)

			// File already exists, update file status and disk size
			if err := u.db.UpdateEpisode(feedID, episode.ID, func(episode *model.Episode) error {
				episode.Size = size
				episode.Status = model.EpisodeDownloaded
				return nil
			}); err != nil {
				logger.WithError(err).Error("failed to update file info")
				return err
			}

			continue
		} else if os.IsNotExist(err) {
			// Will download, do nothing here
		} else {
			logger.WithError(err).Error("failed to stat file")
			return err
		}

		// Download episode to disk
		// We download the episode to a temp directory first to avoid downloading this file by clients
		// while still being processed by youtube-dl (e.g. a file is being downloaded from YT or encoding in progress)

		logger.Infof("! downloading episode %s", episode.VideoURL)
		tempFile, err := u.downloader.Download(ctx, feedConfig, episode)
		if err != nil {
			// YouTube might block host with HTTP Error 429: Too Many Requests
			// We still need to generate XML, so just stop sending download requests and
			// retry next time
			if err == ytdl.ErrTooManyRequests {
				logger.Warn("server responded with a 'Too Many Requests' error")
				break
			}

			// Execute episode download error hooks
			if len(feedConfig.OnEpisodeDownloadError) > 0 {
				env := []string{
					"FEED_NAME=" + feedID,
					"EPISODE_TITLE=" + episode.Title,
					"ERROR_MESSAGE=" + err.Error(),
				}

				for i, hook := range feedConfig.OnEpisodeDownloadError {
					if hookErr := hook.Invoke(env); hookErr != nil {
						logger.Errorf("failed to execute episode download error hook %d: %v", i+1, hookErr)
					} else {
						logger.Infof("episode download error hook %d executed successfully", i+1)
					}
				}
			}

			errMsg := err.Error()
			now := time.Now()
			if err := u.db.UpdateEpisode(feedID, episode.ID, func(episode *model.Episode) error {
				episode.Status = model.EpisodeError
				episode.Error = errMsg
				episode.ErrorAt = &now
				return nil
			}); err != nil {
				return err
			}

			continue
		}

		logger.Debug("copying file")
		fileSize, err := u.fs.Create(ctx, fmt.Sprintf("%s/%s", feedID, episodeName), tempFile)
		tempFile.Close()
		if err != nil {
			logger.WithError(err).Error("failed to copy file")
			return err
		}

		// Execute post episode download hooks
		if len(feedConfig.PostEpisodeDownload) > 0 {
			env := []string{
				"EPISODE_FILE=" + fmt.Sprintf("%s/%s", feedID, episodeName),
				"FEED_NAME=" + feedID,
				"EPISODE_TITLE=" + episode.Title,
			}

			for i, hook := range feedConfig.PostEpisodeDownload {
				if err := hook.Invoke(env); err != nil {
					logger.Errorf("failed to execute post episode download hook %d: %v", i+1, err)
				} else {
					logger.Infof("post episode download hook %d executed successfully", i+1)
				}
			}
		}

		// Update file status in database

		logger.Infof("successfully downloaded file %q", episode.ID)
		if err := u.db.UpdateEpisode(feedID, episode.ID, func(episode *model.Episode) error {
			episode.Size = fileSize
			episode.Status = model.EpisodeDownloaded
			episode.Error = ""
			episode.ErrorAt = nil
			return nil
		}); err != nil {
			return err
		}

		downloaded++
	}

	log.Infof("downloaded %d episode(s)", downloaded)
	return nil
}

func (u *Manager) buildXML(ctx context.Context, feedConfig *feed.Config) error {
	f, err := u.db.GetFeed(ctx, feedConfig.ID)
	if err != nil {
		return err
	}

	// Build iTunes XML feed with data received from builder
	log.Debug("building iTunes podcast feed")
	podcast, err := feed.Build(ctx, f, feedConfig, u.hostname)
	if err != nil {
		return err
	}

	xmlContent := feed.FormatXML(podcast, f)
	var (
		reader  = bytes.NewReader([]byte(xmlContent))
		xmlName = fmt.Sprintf("%s.xml", feedConfig.ID)
	)

	if _, err := u.fs.Create(ctx, xmlName, reader); err != nil {
		return errors.Wrap(err, "failed to upload new XML feed")
	}

	return nil
}

func (u *Manager) buildOPML(ctx context.Context) error {
	// Build OPML with data received from builder
	log.Debug("building podcast OPML")
	u.feedsMu.RLock()
	feeds := make(map[string]*feed.Config, len(u.feeds))
	for id, cfg := range u.feeds {
		feeds[id] = cfg
	}
	u.feedsMu.RUnlock()
	opml, err := feed.BuildOPML(ctx, feeds, u.db, u.hostname)
	if err != nil {
		return err
	}

	var (
		reader  = bytes.NewReader([]byte(opml))
		xmlName = fmt.Sprintf("%s.opml", "podsync")
	)

	if _, err := u.fs.Create(ctx, xmlName, reader); err != nil {
		return errors.Wrap(err, "failed to upload OPML")
	}

	return nil
}

func (u *Manager) cleanup(ctx context.Context, feedConfig *feed.Config) error {
	if info, err := builder.ParseURL(feedConfig.URL); err == nil && info.Provider == model.ProviderAudiobook {
		return nil // Audiobooks are local files and must never be auto-purged
	}

	var (
		feedID = feedConfig.ID
	)

	if feedConfig.Clean == nil {
		return nil
	}

	var keep = feedConfig.Clean.KeepLast
	if keep < 1 {
		return nil
	}

	log.Debugf("purging old episodes, leaving %d", keep)

	// Collect all downloaded episodes from database
	var downloaded []*model.Episode
	err := u.db.WalkEpisodes(ctx, feedID, func(episode *model.Episode) error {
		if episode.Status == model.EpisodeDownloaded {
			downloaded = append(downloaded, episode)
		}
		return nil
	})

	if err != nil {
		return errors.Wrap(err, "failed to build purge list")
	}

	// BadgerDB iterates keys in alphabetical order of episode IDs, not by publication date!
	// We must explicitly sort downloaded episodes by date descending (newest first)
	// so that we retain the newest `keep` episodes and purge only the oldest.
	sort.Slice(downloaded, func(i, j int) bool {
		return downloaded[i].PubDate.After(downloaded[j].PubDate)
	})

	if len(downloaded) <= keep {
		return nil
	}

	purgeList := downloaded[keep:]
	log.Debugf("found %d episode(s) to purge", len(purgeList))

	var result error
	for _, episode := range purgeList {
		episodeName := feed.EpisodeName(feedConfig, episode)
		path := fmt.Sprintf("%s/%s", feedID, episodeName)
		log.Infof("deleting %s", path)
		if err := u.fs.Delete(ctx, path); err != nil {
			result = multierror.Append(result, errors.Wrapf(err, "failed to delete: %s", path))
		}

		if err := u.db.UpdateEpisode(feedID, episode.ID, func(episode *model.Episode) error {
			episode.Status = model.EpisodeCleaned
			episode.Size = 0
			return nil
		}); err != nil {
			result = multierror.Append(result, errors.Wrapf(err, "failed to update episode status: %s", episode.ID))
		}
	}

	return result
}
