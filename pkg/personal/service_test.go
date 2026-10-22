package personal

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/mxpv/podsync/pkg/db"
	"github.com/mxpv/podsync/pkg/fs"
	"github.com/mxpv/podsync/pkg/model"
	"github.com/stretchr/testify/require"
)

type processorFunc func(context.Context, *Media) (io.ReadCloser, error)

func (f processorFunc) Process(ctx context.Context, m *Media) (io.ReadCloser, error) {
	return f(ctx, m)
}
func testService(t *testing.T, processor Processor) (*Service, *db.Badger, fs.Storage) {
	t.Helper()
	database, err := db.NewBadger(&db.Config{Dir: t.TempDir()})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, database.Close()) })
	files, err := fs.NewLocal(t.TempDir(), false, true)
	require.NoError(t, err)
	if processor == nil {
		processor = processorFunc(func(_ context.Context, m *Media) (io.ReadCloser, error) {
			m.Duration = 42
			return io.NopCloser(strings.NewReader("audio")), nil
		})
	}
	service, err := New(context.Background(), database, files, processor, "https://pod.example/podcasts", nil, nil)
	require.NoError(t, err)
	return service, database, files
}
func str(v string) *string                { return &v }
func format(v model.Format) *model.Format { return &v }
func newFeed(t *testing.T, s *Service, title string) *Feed {
	t.Helper()
	f, err := s.Create(context.Background(), Patch{Title: str(title), Format: format(model.FormatAudio)}, nil)
	require.NoError(t, err)
	return f
}
func readFile(t *testing.T, files fs.Storage, path string) string {
	t.Helper()
	f, err := files.Open(path)
	require.NoError(t, err)
	defer f.Close()
	data, err := io.ReadAll(f)
	require.NoError(t, err)
	return string(data)
}
func TestDefaultFeedOnlyOnce(t *testing.T) {
	ctx := context.Background()
	s, database, files := testService(t, nil)
	d, err := s.Get("personal")
	require.NoError(t, err)
	require.Equal(t, "My Feed", d.Feed.Title)
	require.Equal(t, "https://pod.example/podcasts/personal.xml", d.Feed.FeedURL)
	require.Contains(t, readFile(t, files, "personal.xml"), "My Feed")
	require.NoError(t, s.Delete(ctx, "personal", true))
	_, err = files.Size(ctx, "personal.xml")
	require.True(t, os.IsNotExist(err))
	restarted, err := New(ctx, database, files, nil, s.host, nil, nil)
	require.NoError(t, err)
	list, err := restarted.List()
	require.NoError(t, err)
	require.Empty(t, list)
	var marker bool
	require.NoError(t, database.PersonalView(func(tx db.PersonalTx) error { return tx.Get("initialized", &marker) }))
	require.True(t, marker)
}
func TestFirstInitializationCollisionDoesNotOverwrite(t *testing.T) {
	database, err := db.NewBadger(&db.Config{Dir: t.TempDir()})
	require.NoError(t, err)
	defer database.Close()
	files, _ := fs.NewLocal(t.TempDir(), false, true)
	_, err = New(context.Background(), database, files, nil, "https://example.org", map[string]bool{"personal": true}, nil)
	require.ErrorIs(t, err, ErrConflict)
	var initialized bool
	require.ErrorIs(t, database.PersonalView(func(tx db.PersonalTx) error { return tx.Get("initialized", &initialized) }), model.ErrNotFound)
}
func TestCopyMoveAndReferenceSafeDeletion(t *testing.T) {
	ctx := context.Background()
	s, database, files := testService(t, nil)
	target := newFeed(t, s, "IT и разработка")
	ep, err := s.AddURL(ctx, "personal", "https://example.org/video", "Тест")
	require.NoError(t, err)
	require.NoError(t, s.Update(ctx, "personal"))
	d, err := s.Get("personal")
	require.NoError(t, err)
	url := d.Episodes[0].MediaURL
	require.Contains(t, url, "/podcasts/_personal/")
	copy, err := s.Transfer(ctx, "personal", ep.ID, target.ID, false)
	require.NoError(t, err)
	require.NotEqual(t, ep.ID, copy.ID)
	targetDetail, err := s.Get(target.ID)
	require.NoError(t, err)
	require.Equal(t, url, targetDetail.Episodes[0].MediaURL)
	require.Contains(t, readFile(t, files, target.ID+".xml"), "<guid>"+copy.ID+"</guid>")
	require.NoError(t, s.Delete(ctx, "personal", true))
	require.Contains(t, readFile(t, files, target.ID+".xml"), url)
	third := newFeed(t, s, "Позже")
	moved, err := s.Transfer(ctx, target.ID, copy.ID, third.ID, true)
	require.NoError(t, err)
	require.Equal(t, copy.ID, moved.ID)
	targetDetail, err = s.Get(target.ID)
	require.NoError(t, err)
	require.Empty(t, targetDetail.Episodes)
	var media Media
	require.NoError(t, database.PersonalView(func(tx db.PersonalTx) error { return tx.Get("media/"+ep.MediaID, &media) }))
	require.Equal(t, 1, media.References)
	require.Equal(t, "audio", readFile(t, files, media.Path))
	require.NoError(t, s.DeleteEpisode(ctx, third.ID, moved.ID, true))
	_, err = files.Size(ctx, media.Path)
	require.True(t, os.IsNotExist(err))
	require.ErrorIs(t, database.PersonalView(func(tx db.PersonalTx) error { return tx.Get("media/"+ep.MediaID, &media) }), model.ErrNotFound)
}
func TestRetainFilesAndPatchRebuild(t *testing.T) {
	ctx := context.Background()
	s, _, files := testService(t, nil)
	ep, err := s.Upload(ctx, "personal", "chapter.mp3", strings.NewReader("source"))
	require.NoError(t, err)
	require.NoError(t, s.Update(ctx, "personal"))
	before, err := s.Get("personal")
	require.NoError(t, err)
	private := true
	updated, err := s.Patch(ctx, "personal", Patch{Title: str("Новое название"), Description: str("Описание"), PrivateFeed: &private})
	require.NoError(t, err)
	require.Equal(t, "personal", updated.ID)
	xml := readFile(t, files, "personal.xml")
	require.Contains(t, xml, "Новое название")
	require.Contains(t, xml, "<itunes:block>yes</itunes:block>")
	require.Contains(t, xml, "<guid>"+ep.ID+"</guid>")
	require.NoError(t, s.DeleteEpisode(ctx, "personal", ep.ID, false))
	path := strings.TrimPrefix(before.Episodes[0].MediaURL, s.host+"/")
	require.Equal(t, "audio", readFile(t, files, path))
}
func TestFormatChangesDoNotRelabelSharedMedia(t *testing.T) {
	ctx := context.Background()
	s, _, files := testService(t, nil)
	ep, err := s.AddURL(ctx, "personal", "https://example.org/a", "Audio")
	require.NoError(t, err)
	require.NoError(t, s.Update(ctx, "personal"))
	target, err := s.Create(ctx, Patch{Title: str("Video"), Format: format(model.FormatVideo)}, nil)
	require.NoError(t, err)
	_, err = s.Transfer(ctx, "personal", ep.ID, target.ID, false)
	require.NoError(t, err)
	xml := readFile(t, files, target.ID+".xml")
	require.Contains(t, xml, `type="audio/mpeg"`)
	require.NotContains(t, xml, `type="video/mp4"`)
}
func TestConcurrentCopiesAndDeletion(t *testing.T) {
	ctx := context.Background()
	s, database, _ := testService(t, nil)
	ep, err := s.AddURL(ctx, "personal", "https://example.org/a", "Audio")
	require.NoError(t, err)
	target := newFeed(t, s, "Target")
	var wg sync.WaitGroup
	failures := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Transfer(ctx, "personal", ep.ID, target.ID, false)
			failures <- err
		}()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	var media Media
	require.NoError(t, database.PersonalView(func(tx db.PersonalTx) error { return tx.Get("media/"+ep.MediaID, &media) }))
	require.Equal(t, 21, media.References)
	require.NoError(t, s.Delete(ctx, target.ID, true))
	require.NoError(t, database.PersonalView(func(tx db.PersonalTx) error { return tx.Get("media/"+ep.MediaID, &media) }))
	require.Equal(t, 1, media.References)
}
func TestDeleteWhileDownloadRunning(t *testing.T) {
	started := make(chan struct{})
	resume := make(chan struct{})
	processor := processorFunc(func(_ context.Context, _ *Media) (io.ReadCloser, error) {
		close(started)
		<-resume
		return io.NopCloser(strings.NewReader("done")), nil
	})
	ctx := context.Background()
	s, database, files := testService(t, processor)
	ep, err := s.AddURL(ctx, "personal", "https://example.org/a", "Audio")
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- s.Update(ctx, "personal") }()
	<-started
	require.NoError(t, s.Delete(ctx, "personal", true))
	close(resume)
	require.NoError(t, <-done)
	_, err = files.Size(ctx, "personal.xml")
	require.True(t, os.IsNotExist(err))
	var media Media
	require.ErrorIs(t, database.PersonalView(func(tx db.PersonalTx) error { return tx.Get("media/"+ep.MediaID, &media) }), model.ErrNotFound)
}
func TestRetryAndValidation(t *testing.T) {
	fail := true
	processor := processorFunc(func(_ context.Context, _ *Media) (io.ReadCloser, error) {
		if fail {
			return nil, errors.New("network failed")
		}
		return io.NopCloser(strings.NewReader("done")), nil
	})
	ctx := context.Background()
	s, _, _ := testService(t, processor)
	for _, source := range []string{"file:///etc/passwd", "ftp://example.org/a", "http://user:pass@example.org/a", "--exec=x"} {
		_, err := s.AddURL(ctx, "personal", source, "")
		require.ErrorIs(t, err, ErrInvalid)
	}
	for _, name := range []string{"../../evil.mp3", "..\\evil.mp3", "file.html"} {
		_, err := s.Upload(ctx, "personal", name, strings.NewReader("x"))
		require.ErrorIs(t, err, ErrInvalid)
	}
	_, err := s.Create(ctx, Patch{Title: str("Title")}, nil)
	require.ErrorIs(t, err, ErrInvalid)
	_, err = s.Patch(ctx, "personal", Patch{Title: str("  ")})
	require.ErrorIs(t, err, ErrInvalid)
	ep, err := s.AddURL(ctx, "personal", "https://example.org/a", "Audio")
	require.NoError(t, err)
	require.Error(t, s.Update(ctx, "personal"))
	d, err := s.Get("personal")
	require.NoError(t, err)
	require.Equal(t, model.EpisodeError, d.Episodes[0].Status)
	fail = false
	require.NoError(t, s.Retry(ctx, "personal", ep.ID))
	require.NoError(t, s.Update(ctx, "personal"))
	d, err = s.Get("personal")
	require.NoError(t, err)
	require.Equal(t, model.EpisodeDownloaded, d.Episodes[0].Status)
}
func TestAtomicTransferRollback(t *testing.T) {
	ctx := context.Background()
	s, database, _ := testService(t, nil)
	ep, err := s.AddURL(ctx, "personal", "https://example.org/a", "Audio")
	require.NoError(t, err)
	_, err = s.Transfer(ctx, "personal", ep.ID, "missing", true)
	require.ErrorIs(t, err, model.ErrNotFound)
	d, err := s.Get("personal")
	require.NoError(t, err)
	require.Len(t, d.Episodes, 1)
	var count int
	require.NoError(t, database.PersonalView(func(tx db.PersonalTx) error {
		return tx.Walk("media/", func(_ string, data []byte) error {
			var m Media
			if err := json.Unmarshal(data, &m); err != nil {
				return err
			}
			count += m.References
			return nil
		})
	}))
	require.Equal(t, 1, count)
}

func TestMediaPathsStayInsideSharedStorage(t *testing.T) {
	for _, name := range []string{"../secret", "_personal/../secret", "/etc/passwd", "_personal"} {
		require.Error(t, validateMediaPath(name), name)
	}
	require.NoError(t, validateMediaPath("_personal/abc/media.mp3"))
}
