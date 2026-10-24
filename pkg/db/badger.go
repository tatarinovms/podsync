package db

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/dgraph-io/badger"
	"github.com/dgraph-io/badger/options"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/mxpv/podsync/pkg/model"
)

const (
	versionPath               = "podsync/version"
	feedPrefix                = "feed/"
	feedPath                  = "feed/%s"
	statsPrefix               = "stats/"
	statsPath                 = "stats/%s"
	episodePrefix             = "episode/%s/"
	episodePath               = "episode/%s/%s" // FeedID + EpisodeID
	jobPrefix                 = "job/"
	jobPath                   = "job/%s"
	activeJobPrefix           = "job_active/"
	activeJobPath             = "job_active/%s"
	activeJobIndexVersionPath = "index/job_active/v1"
)

// BadgerConfig represents BadgerDB configuration parameters
type BadgerConfig struct {
	Truncate bool `toml:"truncate"`
	FileIO   bool `toml:"file_io"`
}

type Badger struct {
	db          *badger.DB
	lifecycleMu sync.RWMutex
	closed      bool
}

var _ Storage = (*Badger)(nil)

func NewBadger(config *Config) (*Badger, error) {
	var (
		dir = config.Dir
	)

	log.Infof("opening database %q", dir)

	// Make sure database directory exists
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, errors.Wrap(err, "could not mkdir database dir")
	}

	opts := badger.DefaultOptions(dir).
		WithLogger(log.StandardLogger()).
		WithTruncate(true)

	if config.Badger != nil {
		opts.Truncate = config.Badger.Truncate
		if config.Badger.FileIO {
			opts.ValueLogLoadingMode = options.FileIO
		}
	}

	db, err := badger.Open(opts)
	if err != nil {
		return nil, errors.Wrap(err, "failed to open database")
	}

	storage := &Badger{db: db}

	if err := db.Update(func(txn *badger.Txn) error {
		if err := storage.setObj(txn, []byte(versionPath), CurrentVersion, false); err != nil && err != model.ErrAlreadyExists {
			return err
		}
		return nil
	}); err != nil {
		_ = db.Close()
		return nil, errors.Wrap(err, "failed to read database version")
	}

	if err := storage.initActiveJobsIndex(); err != nil {
		_ = db.Close()
		return nil, errors.Wrap(err, "failed to initialize active jobs index")
	}

	return storage, nil
}

// Run before workers start. A marker is committed only after all batches succeed,
// so an interrupted migration can be retried without losing legacy jobs.
func (b *Badger) initActiveJobsIndex() error {
	marker := b.getKey(activeJobIndexVersionPath)
	var initialized bool
	if err := b.db.View(func(txn *badger.Txn) error {
		err := b.getObj(txn, marker, &initialized)
		if err == model.ErrNotFound {
			return nil
		}
		return err
	}); err != nil || initialized {
		return err
	}
	batch := b.db.NewWriteBatch()
	defer batch.Cancel()
	err := b.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = b.getKey(activeJobPrefix)
		if err := b.iterator(context.Background(), txn, opts, func(item *badger.Item) error {
			return batch.Delete(item.KeyCopy(nil))
		}); err != nil {
			return err
		}
		opts.Prefix = b.getKey(jobPrefix)
		return b.iterator(context.Background(), txn, opts, func(item *badger.Item) error {
			var job model.UpdateJob
			if err := b.unmarshalObj(item, &job); err != nil {
				return err
			}
			if !job.IsActive() {
				return nil
			}
			data, err := b.marshalObj(&job)
			if err != nil {
				return err
			}
			return batch.Set(b.getKey(activeJobPath, job.ID), data)
		})
	})
	if err != nil {
		return err
	}
	if err := batch.Flush(); err != nil {
		return err
	}
	return b.db.Update(func(txn *badger.Txn) error {
		return b.setObj(txn, marker, true, true)
	})
}

// Episode mutations now share the feed's stats key. Retry optimistic conflicts
// even when workers modify different episodes of the same feed.
func (b *Badger) update(ctx context.Context, fn func(*badger.Txn) error) error {
	for attempt := 0; attempt < 32; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := b.db.Update(fn)
		if err != badger.ErrConflict {
			return err
		}
	}
	return badger.ErrConflict
}

func (b *Badger) Close() error {
	b.lifecycleMu.Lock()
	defer b.lifecycleMu.Unlock()
	if b.closed {
		return nil
	}
	b.closed = true
	log.Debug("closing database")
	return b.db.Close()
}

func (b *Badger) Version() (int, error) {
	b.lifecycleMu.RLock()
	defer b.lifecycleMu.RUnlock()
	if b.closed {
		return -1, errors.New("database is closed")
	}
	var (
		version = -1
	)

	err := b.db.View(func(txn *badger.Txn) error {
		return b.getObj(txn, []byte(versionPath), &version)
	})

	return version, err
}

func (b *Badger) AddFeed(ctx context.Context, feedID string, feed *model.Feed) error {
	return b.update(ctx, func(txn *badger.Txn) error {
		stats, err := b.feedStats(ctx, txn, feedID)
		if err != nil {
			return err
		}
		// Insert or update feed info
		feedKey := b.getKey(feedPath, feedID)
		if err := b.setObj(txn, feedKey, feed, true); err != nil {
			return err
		}

		// A local book is a complete snapshot of its directory. Synchronize it
		// atomically, while remote feeds retain downloaded historical episodes.
		isBook := feed.Provider == model.ProviderAudiobook
		if isBook && feed.Episodes != nil {
			keep := make(map[string]bool, len(feed.Episodes))
			for _, episode := range feed.Episodes {
				keep[episode.ID] = true
			}
			opts := badger.DefaultIteratorOptions
			opts.Prefix = b.getKey(episodePrefix, feedID)
			if err := b.iterator(ctx, txn, opts, func(item *badger.Item) error {
				var old model.Episode
				if err := b.unmarshalObj(item, &old); err != nil {
					return err
				}
				if !keep[old.ID] {
					adjustStats(stats, &old, -1)
					return txn.Delete(item.KeyCopy(nil))
				}
				return nil
			}); err != nil {
				return err
			}
		}
		// Append remote episodes; refresh all chapter metadata for local books.
		for _, episode := range feed.Episodes {
			if err := ctx.Err(); err != nil {
				return err
			}
			episodeKey := b.getKey(episodePath, feedID, episode.ID)
			var old model.Episode
			if err := b.getObj(txn, episodeKey, &old); err == nil {
				if !isBook {
					continue
				}
				adjustStats(stats, &old, -1)
			} else if err != model.ErrNotFound {
				return err
			}
			if err := b.setObj(txn, episodeKey, episode, true); err != nil {
				return errors.Wrapf(err, "failed to save episode %q", feedID)
			}
			adjustStats(stats, episode, 1)
		}

		return b.setObj(txn, b.getKey(statsPath, feedID), stats, true)
	})
}

func adjustStats(stats *model.FeedStats, ep *model.Episode, delta int) {
	stats.EpisodeCount += delta
	switch ep.Status {
	case model.EpisodeDownloaded:
		stats.DownloadedCount += delta
		stats.TotalSize += int64(delta) * ep.Size
	case model.EpisodeError:
		stats.ErrorCount += delta
	}
}

func (b *Badger) feedStats(ctx context.Context, txn *badger.Txn, feedID string) (*model.FeedStats, error) {
	stats := &model.FeedStats{}
	if err := b.getObj(txn, b.getKey(statsPath, feedID), stats); err == nil {
		return stats, nil
	} else if err != model.ErrNotFound {
		return nil, err
	}
	// Legacy databases have no stats yet. Rebuild in the mutation's snapshot.
	err := b.walkEpisodes(ctx, txn, feedID, func(ep *model.Episode) error {
		adjustStats(stats, ep, 1)
		return nil
	})
	return stats, err
}

func (b *Badger) GetFeed(ctx context.Context, feedID string) (*model.Feed, error) {
	var (
		feed    = model.Feed{}
		feedKey = b.getKey(feedPath, feedID)
	)

	if err := b.db.View(func(txn *badger.Txn) error {
		// Query feed
		if err := b.getObj(txn, feedKey, &feed); err != nil {
			return err
		}

		// Query episodes
		if err := b.walkEpisodes(ctx, txn, feedID, func(episode *model.Episode) error {
			feed.Episodes = append(feed.Episodes, episode)
			return nil
		}); err != nil {
			return err
		}

		return nil
	}); err != nil {
		return nil, err
	}

	return &feed, nil
}

func (b *Badger) GetFeedMetadata(_ context.Context, feedID string) (*model.Feed, error) {
	var (
		feed    = model.Feed{}
		feedKey = b.getKey(feedPath, feedID)
	)

	if err := b.db.View(func(txn *badger.Txn) error {
		return b.getObj(txn, feedKey, &feed)
	}); err != nil {
		return nil, err
	}

	return &feed, nil
}

func (b *Badger) GetFeedStats(ctx context.Context, feedID string) (*model.FeedStats, error) {
	var (
		stats    = model.FeedStats{}
		statsKey = b.getKey(statsPath, feedID)
	)

	found := false
	err := b.db.View(func(txn *badger.Txn) error {
		if err := b.getObj(txn, statsKey, &stats); err == nil {
			found = true
			return nil
		} else if err != model.ErrNotFound {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if found {
		return &stats, nil
	}

	// Compute on the fly if missing (self-healing for legacy database records)
	err = b.update(ctx, func(txn *badger.Txn) error {
		current, err := b.feedStats(ctx, txn, feedID)
		if err != nil {
			return err
		}
		stats = *current
		return b.setObj(txn, statsKey, &stats, true)
	})
	if err != nil {
		return nil, err
	}

	return &stats, nil
}

func (b *Badger) WalkFeeds(ctx context.Context, cb func(feed *model.Feed) error) error {
	return b.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = b.getKey(feedPrefix)
		opts.PrefetchValues = true
		return b.iterator(ctx, txn, opts, func(item *badger.Item) error {
			feed := &model.Feed{}
			if err := b.unmarshalObj(item, feed); err != nil {
				return err
			}

			return cb(feed)
		})
	})
}

func (b *Badger) DeleteFeed(ctx context.Context, feedID string) error {
	return b.update(ctx, func(txn *badger.Txn) error {
		if _, err := txn.Get(b.getKey(statsPath, feedID)); err != nil && err != badger.ErrKeyNotFound {
			return err
		}
		// Feed
		feedKey := b.getKey(feedPath, feedID)
		if err := txn.Delete(feedKey); err != nil {
			return errors.Wrapf(err, "failed to delete feed %q", feedID)
		}

		// Stats
		statsKey := b.getKey(statsPath, feedID)
		if err := txn.Delete(statsKey); err != nil {
			return err
		}

		// Episodes
		opts := badger.DefaultIteratorOptions
		opts.Prefix = b.getKey(episodePrefix, feedID)
		opts.PrefetchValues = false
		if err := b.iterator(ctx, txn, opts, func(item *badger.Item) error {
			return txn.Delete(item.KeyCopy(nil))
		}); err != nil {
			return errors.Wrapf(err, "failed to iterate episodes for feed %q", feedID)
		}

		return nil
	})
}

func (b *Badger) GetEpisode(_ context.Context, feedID string, episodeID string) (*model.Episode, error) {
	var (
		episode model.Episode
		err     error
		key     = b.getKey(episodePath, feedID, episodeID)
	)

	err = b.db.View(func(txn *badger.Txn) error {
		return b.getObj(txn, key, &episode)
	})

	return &episode, err
}

func (b *Badger) UpdateEpisode(feedID string, episodeID string, cb func(episode *model.Episode) error) error {
	key := b.getKey(episodePath, feedID, episodeID)

	return b.update(context.Background(), func(txn *badger.Txn) error {
		var episode model.Episode
		if err := b.getObj(txn, key, &episode); err != nil {
			return err
		}

		stats, err := b.feedStats(context.Background(), txn, feedID)
		if err != nil {
			return err
		}
		old := episode

		if err := cb(&episode); err != nil {
			return err
		}

		if episode.ID != episodeID {
			return errors.New("can't change episode ID")
		}

		if err := b.setObj(txn, key, &episode, true); err != nil {
			return err
		}

		adjustStats(stats, &old, -1)
		adjustStats(stats, &episode, 1)
		return b.setObj(txn, b.getKey(statsPath, feedID), stats, true)
	})
}

func (b *Badger) DeleteEpisode(feedID, episodeID string) error {
	key := b.getKey(episodePath, feedID, episodeID)
	statsKey := b.getKey(statsPath, feedID)
	return b.update(context.Background(), func(txn *badger.Txn) error {
		var ep model.Episode
		if err := b.getObj(txn, key, &ep); err == model.ErrNotFound {
			return nil
		} else if err != nil {
			return err
		}
		stats, err := b.feedStats(context.Background(), txn, feedID)
		if err != nil {
			return err
		}
		adjustStats(stats, &ep, -1)
		if err := b.setObj(txn, statsKey, stats, true); err != nil {
			return err
		}
		return txn.Delete(key)
	})
}

func (b *Badger) WalkEpisodes(ctx context.Context, feedID string, cb func(episode *model.Episode) error) error {
	return b.db.View(func(txn *badger.Txn) error {
		return b.walkEpisodes(ctx, txn, feedID, cb)
	})
}

func (b *Badger) walkEpisodes(ctx context.Context, txn *badger.Txn, feedID string, cb func(episode *model.Episode) error) error {
	opts := badger.DefaultIteratorOptions
	opts.Prefix = b.getKey(episodePrefix, feedID)
	opts.PrefetchValues = true
	return b.iterator(ctx, txn, opts, func(item *badger.Item) error {
		feed := &model.Episode{}
		if err := b.unmarshalObj(item, feed); err != nil {
			return err
		}

		return cb(feed)
	})
}

func (b *Badger) SaveJob(_ context.Context, job *model.UpdateJob) error {
	if job == nil || job.ID == "" {
		return errors.New("invalid job or empty job ID")
	}
	key := b.getKey(jobPath, job.ID)
	activeKey := b.getKey(activeJobPath, job.ID)
	return b.db.Update(func(txn *badger.Txn) error {
		if err := b.setObj(txn, key, job, true); err != nil {
			return err
		}
		if job.IsActive() {
			return b.setObj(txn, activeKey, job, true)
		}
		return txn.Delete(activeKey)
	})
}

func (b *Badger) WalkActiveJobs(ctx context.Context, cb func(job *model.UpdateJob) error) error {
	return b.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = b.getKey(activeJobPrefix)
		opts.PrefetchValues = true
		return b.iterator(ctx, txn, opts, func(item *badger.Item) error {
			job := &model.UpdateJob{}
			if err := b.unmarshalObj(item, job); err != nil {
				return err
			}
			return cb(job)
		})
	})
}

func (b *Badger) GetJob(_ context.Context, jobID string) (*model.UpdateJob, error) {
	key := b.getKey(jobPath, jobID)
	var job model.UpdateJob
	err := b.db.View(func(txn *badger.Txn) error {
		return b.getObj(txn, key, &job)
	})
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func (b *Badger) WalkJobs(ctx context.Context, cb func(job *model.UpdateJob) error) error {
	return b.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = b.getKey(jobPrefix)
		opts.PrefetchValues = true
		return b.iterator(ctx, txn, opts, func(item *badger.Item) error {
			job := &model.UpdateJob{}
			if err := b.unmarshalObj(item, job); err != nil {
				return err
			}
			return cb(job)
		})
	})
}

func (b *Badger) DeleteJob(_ context.Context, jobID string) error {
	key := b.getKey(jobPath, jobID)
	activeKey := b.getKey(activeJobPath, jobID)
	return b.db.Update(func(txn *badger.Txn) error {
		if err := txn.Delete(activeKey); err != nil {
			return err
		}
		return txn.Delete(key)
	})
}

func (b *Badger) iterator(ctx context.Context, txn *badger.Txn, opts badger.IteratorOptions, callback func(item *badger.Item) error) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	iter := txn.NewIterator(opts)
	defer iter.Close()

	for iter.Rewind(); iter.Valid(); iter.Next() {
		if ctx != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		}
		item := iter.Item()

		if err := callback(item); err != nil {
			return err
		}
	}

	return nil
}

func (b *Badger) getKey(format string, a ...interface{}) []byte {
	resourcePath := fmt.Sprintf(format, a...)
	fullPath := fmt.Sprintf("podsync/v%d/%s", CurrentVersion, resourcePath)

	return []byte(fullPath)
}

func (b *Badger) setObj(txn *badger.Txn, key []byte, obj interface{}, overwrite bool) error {
	if !overwrite {
		// Overwrites are not allowed, make sure there is no object with the given key
		_, err := txn.Get(key)
		switch err {
		case nil:
			return model.ErrAlreadyExists
		case badger.ErrKeyNotFound:
			// Key not found, do nothing
		default:
			return errors.Wrap(err, "failed to check whether key exists")
		}
	}

	data, err := b.marshalObj(obj)
	if err != nil {
		return errors.Wrapf(err, "failed to serialize object for key %q", key)
	}

	return txn.Set(key, data)
}

func (b *Badger) getObj(txn *badger.Txn, key []byte, out interface{}) error {
	item, err := txn.Get(key)
	if err != nil {
		if err == badger.ErrKeyNotFound {
			return model.ErrNotFound
		}

		return err
	}

	return b.unmarshalObj(item, out)
}

func (b *Badger) marshalObj(obj interface{}) ([]byte, error) {
	return json.Marshal(obj)
}

func (b *Badger) unmarshalObj(item *badger.Item, out interface{}) error {
	return item.Value(func(val []byte) error {
		return json.Unmarshal(val, out)
	})
}
