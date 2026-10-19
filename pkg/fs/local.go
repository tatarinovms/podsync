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

func (l *Local) resolvePath(name string) (string, error) {
	cleanName := filepath.Clean(filepath.FromSlash(strings.TrimPrefix(name, "/")))
	root := filepath.Clean(l.rootDir)
	if cleanName == "." || cleanName == "" {
		return root, nil
	}
	fullPath := filepath.Join(root, cleanName)
	rel, err := filepath.Rel(root, fullPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", errors.Errorf("invalid path traversal %q", name)
	}
	return fullPath, nil
}

func (l *Local) Open(name string) (http.File, error) {
	if l.WebUIEnabled {
		switch name {
		case "/index.html", "/admin.css", "/admin.js", "/theme.js":
			return os.Open(filepath.Join("html", strings.TrimPrefix(name, "/")))
		}
	}

	cleanName := filepath.FromSlash(strings.TrimPrefix(name, "/"))
	parts := strings.Split(cleanName, string(filepath.Separator))

	if len(parts) > 1 {
		l.mountLock.RLock()
		targetDir, ok := l.mounts[parts[0]]
		l.mountLock.RUnlock()
		if ok {
			rest := filepath.Join(parts[1:]...)
			mountedPath := filepath.Join(targetDir, rest)
			file, err := os.Open(mountedPath)
			if err == nil {
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
		}
	}

	path, err := l.resolvePath(name)
	if err != nil {
		return nil, os.ErrNotExist
	}
	file, err := os.Open(path)
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
	path, err := l.resolvePath(name)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("failed to delete file %s: %w", path, err)
	}
	return nil
}

func (l *Local) Create(_ctx context.Context, name string, reader io.Reader) (int64, error) {
	path, err := l.resolvePath(name)
	if err != nil {
		return 0, err
	}
	logger := log.WithField("name", name)

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return 0, errors.Wrapf(err, "failed to mkdir: %s", path)
	}

	logger.Infof("creating file: %s", path)
	tmpPath := fmt.Sprintf("%s.tmp.%d", path, time.Now().UnixNano())
	written, err := l.copyFile(reader, tmpPath)
	if err != nil {
		_ = os.Remove(tmpPath)
		return 0, errors.Wrap(err, "failed to copy file")
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return 0, errors.Wrap(err, "failed to rename temp file to target")
	}

	logger.Debugf("written %d bytes", written)
	return written, nil
}

func (l *Local) copyFile(source io.Reader, destinationPath string) (int64, error) {
	dest, err := os.Create(destinationPath)
	if err != nil {
		return 0, errors.Wrap(err, "failed to create destination file")
	}

	defer dest.Close()

	written, err := io.Copy(dest, source)
	if err != nil {
		return 0, errors.Wrap(err, "failed to copy data")
	}

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
