package fs

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

// LocalConfig is the storage configuration for local file system
type LocalConfig struct {
	DataDir string `toml:"data_dir"`
}

// Local implements local file storage
type Local struct {
	rootDir      string
	WebUIEnabled bool
	NoListing    bool
	mounts       map[string]string
	mountLock    sync.RWMutex
}

func NewLocal(rootDir string, webUIEnabled bool, noListing bool) (*Local, error) {
	return &Local{
		rootDir:      rootDir,
		WebUIEnabled: webUIEnabled,
		NoListing:    noListing,
		mounts:       make(map[string]string),
	}, nil
}

func (l *Local) Mount(prefix string, dirPath string) {
	l.mountLock.Lock()
	defer l.mountLock.Unlock()
	if l.mounts == nil {
		l.mounts = make(map[string]string)
	}
	l.mounts[prefix] = dirPath
}

func (l *Local) Unmount(prefix string) {
	l.mountLock.Lock()
	defer l.mountLock.Unlock()
	delete(l.mounts, prefix)
}

func (l *Local) GetMount(prefix string) (string, bool) {
	l.mountLock.RLock()
	defer l.mountLock.RUnlock()
	dir, ok := l.mounts[prefix]
	return dir, ok
}

// resolveTarget returns the root path and relative target path within that root.
// Both base data directory and mounted directories are treated as secure roots.
func (l *Local) resolveTarget(name string) (string, string, error) {
	cleanName := filepath.Clean(filepath.FromSlash(strings.TrimPrefix(name, "/")))
	if cleanName == "." || cleanName == "" {
		if l.rootDir == "" {
			return "", "", os.ErrNotExist
		}
		return l.rootDir, ".", nil
	}

	parts := strings.Split(cleanName, string(filepath.Separator))
	if len(parts) > 0 {
		l.mountLock.RLock()
		targetDir, ok := l.mounts[parts[0]]
		l.mountLock.RUnlock()
		if ok {
			rest := "."
			if len(parts) > 1 {
				rest = filepath.Join(parts[1:]...)
				if rest == "" {
					rest = "."
				}
			}
			return targetDir, rest, nil
		}
	}

	if l.rootDir == "" {
		return "", "", os.ErrNotExist
	}
	return l.rootDir, cleanName, nil
}

func (l *Local) Open(name string) (http.File, error) {
	if l.WebUIEnabled {
		switch name {
		case "/index.html", "/admin.css", "/admin.js", "/theme.js":
			return os.Open(filepath.Join("html", strings.TrimPrefix(name, "/")))
		}
	}

	rootPath, relPath, err := l.resolveTarget(name)
	if err != nil {
		return nil, os.ErrNotExist
	}

	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, err
	}
	defer root.Close()

	file, err := root.Open(relPath)
	if err != nil {
		return nil, err
	}

	// If NoListing is enabled, prevent directory listings by returning 404
	if l.NoListing {
		stat, err := file.Stat()
		if err != nil {
			file.Close()
			return nil, err
		}
		if stat.IsDir() {
			file.Close()
			return nil, os.ErrNotExist
		}
	}

	return file, nil
}

func (l *Local) Delete(_ctx context.Context, name string) error {
	rootPath, relPath, err := l.resolveTarget(name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return os.ErrNotExist
		}
		return err
	}

	root, err := os.OpenRoot(rootPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return os.ErrNotExist
		}
		return err
	}
	defer root.Close()

	if err := root.Remove(relPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return os.ErrNotExist
		}
		return fmt.Errorf("failed to delete file %s: %w", name, err)
	}
	return nil
}

func (l *Local) Create(_ctx context.Context, name string, reader io.Reader) (int64, error) {
	rootPath, relPath, err := l.resolveTarget(name)
	if err != nil {
		return 0, err
	}

	if err := os.MkdirAll(rootPath, 0755); err != nil {
		return 0, errors.Wrapf(err, "failed to mkdir root: %s", rootPath)
	}

	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return 0, err
	}
	defer root.Close()

	dir := filepath.Dir(relPath)
	if dir != "." && dir != "" {
		if err := root.MkdirAll(dir, 0755); err != nil {
			return 0, errors.Wrapf(err, "failed to mkdir: %s", dir)
		}
	}

	logger := log.WithField("name", name)
	logger.Infof("creating file: %s in root %s", relPath, rootPath)

	tmpRelPath := fmt.Sprintf("%s.tmp.%d", relPath, time.Now().UnixNano())
	dest, err := root.OpenFile(tmpRelPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return 0, errors.Wrap(err, "failed to create destination file")
	}

	written, copyErr := io.Copy(dest, reader)
	closeErr := dest.Close()
	if copyErr != nil {
		_ = root.Remove(tmpRelPath)
		return 0, errors.Wrap(copyErr, "failed to copy data")
	}
	if closeErr != nil {
		_ = root.Remove(tmpRelPath)
		return 0, errors.Wrap(closeErr, "failed to close destination file")
	}

	if err := root.Rename(tmpRelPath, relPath); err != nil {
		_ = root.Remove(tmpRelPath)
		return 0, errors.Wrap(err, "failed to rename temp file to target")
	}

	logger.Debugf("written %d bytes", written)
	return written, nil
}

func (l *Local) Size(_ctx context.Context, name string) (int64, error) {
	file, err := l.Open(name)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return 0, err
	}

	return stat.Size(), nil
}
