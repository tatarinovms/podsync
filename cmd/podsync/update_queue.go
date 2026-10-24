package main

import (
	"context"

	"github.com/mxpv/podsync/pkg/feed"
	"github.com/pkg/errors"
)

// Called while holding m.mu, before persisting changes that require a rebuild.
func (m *AppFeedManager) checkUpdateCapacityLocked() error {
	if m.queue != nil {
		return nil
	}
	if m.updates != nil && len(m.updates) >= cap(m.updates) {
		return errors.New("update queue is full; no settings were changed, try again shortly")
	}
	return nil
}

func (m *AppFeedManager) queueUpdateLocked(f *feed.Config) error {
	if m.queue != nil {
		_, created, err := m.queue.EnqueueManualUpdate(context.Background(), f.ID)
		if err != nil {
			return err
		}
		if created {
			m.markQueuedLocked(f.ID)
		}
		return nil
	}
	if m.updates == nil {
		return nil
	}
	m.markQueuedLocked(f.ID)
	select {
	case m.updates <- f:
		return nil
	default:
		m.queuedUpdates[f.ID]--
		return errors.New("settings saved, but update queue is full; trigger a manual update")
	}
}
