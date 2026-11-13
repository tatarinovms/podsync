package web

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
)

func init() {
	_ = mime.AddExtensionType(".js", "application/javascript; charset=utf-8")
	_ = mime.AddExtensionType(".css", "text/css; charset=utf-8")
	_ = mime.AddExtensionType(".woff2", "font/woff2")
	_ = mime.AddExtensionType(".woff", "font/woff")
	_ = mime.AddExtensionType(".svg", "image/svg+xml")
}

//go:embed ui/*
var adminContent embed.FS

// adminAsset is a single embedded UI file prepared for efficient delivery.
//
// The admin console ships roughly a megabyte of JavaScript, CSS and fonts.
// Reading and compressing those files once at startup keeps every request
// allocation-free, while a content-derived ETag lets browsers revalidate the
// shell with a 304 response instead of downloading it again.
type adminAsset struct {
	name        string
	contentType string
	content     []byte
	gzipContent []byte
	etag        string
	immutable   bool
}

var (
	adminAssetsOnce sync.Once
	adminAssets     map[string]*adminAsset
	adminAssetsErr  error
)

// adminAssetIndex loads the embedded admin console once. Callers receive a
// read-only map that is never mutated after initialization.
func adminAssetIndex() (map[string]*adminAsset, error) {
	adminAssetsOnce.Do(func() {
		sub, err := fs.Sub(adminContent, "ui")
		if err != nil {
			adminAssetsErr = err
			return
		}
		assets := make(map[string]*adminAsset)
		err = fs.WalkDir(sub, ".", func(name string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			data, err := fs.ReadFile(sub, name)
			if err != nil {
				return err
			}
			assets[name] = newAdminAsset(name, data)
			return nil
		})
		if err != nil {
			adminAssetsErr = err
			return
		}
		adminAssets = assets
	})
	return adminAssets, adminAssetsErr
}

func newAdminAsset(name string, content []byte) *adminAsset {
	sum := sha256.Sum256(content)
	asset := &adminAsset{
		name:        name,
		contentType: adminContentType(name),
		content:     content,
		etag:        `"` + hex.EncodeToString(sum[:16]) + `"`,
		immutable:   strings.HasPrefix(assetExtension(name), ".woff"),
	}
	if adminAssetCompressible(name) {
		var compressed bytes.Buffer
		writer, err := gzip.NewWriterLevel(&compressed, gzip.DefaultCompression)
		if err == nil {
			if _, err = writer.Write(content); err == nil {
				if err = writer.Close(); err == nil && compressed.Len() < len(content) {
					asset.gzipContent = compressed.Bytes()
				}
			}
		}
	}
	return asset
}

func adminContentType(name string) string {
	if contentType := mime.TypeByExtension(path.Ext(name)); contentType != "" {
		return contentType
	}
	switch path.Ext(name) {
	case ".html":
		return "text/html; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}

func assetExtension(name string) string {
	return strings.ToLower(path.Ext(name))
}

func adminAssetCompressible(name string) bool {
	switch assetExtension(name) {
	case ".html", ".js", ".css", ".json", ".svg", ".txt", ".xml", ".map":
		return true
	default:
		return false
	}
}

// adminUIHandler serves the embedded admin console. Missing files fall back to
// index.html so client-side routes keep working, while requests for missing
// assets (paths with an extension) return 404 instead of HTML.
func (s *Server) adminUIHandler() http.Handler {
	assets, err := adminAssetIndex()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err != nil || len(assets) == 0 {
			http.Error(w, "admin console assets unavailable", http.StatusInternalServerError)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		name := strings.TrimPrefix(r.URL.Path, "/admin")
		name = strings.TrimPrefix(name, "/")
		if name == "" || name == "login" || name == "index.html" {
			name = "index.html"
		}

		asset, ok := assets[name]
		if !ok {
			if path.Ext(name) != "" {
				http.NotFound(w, r)
				return
			}
			asset = assets["index.html"]
			if asset == nil {
				http.NotFound(w, r)
				return
			}
		}
		asset.serveHTTP(w, r)
	})
}

func (a *adminAsset) serveHTTP(w http.ResponseWriter, r *http.Request) {
	header := w.Header()
	header.Set("Content-Type", a.contentType)
	header.Set("ETag", a.etag)
	header.Set("Vary", "Accept-Encoding")
	header.Set("X-Content-Type-Options", "nosniff")
	if a.immutable {
		header.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		header.Set("Cache-Control", "no-cache, must-revalidate")
	}

	if etagMatches(r.Header.Get("If-None-Match"), a.etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	body := a.content
	if a.gzipContent != nil && acceptsGzip(r.Header.Get("Accept-Encoding")) {
		header.Set("Content-Encoding", "gzip")
		body = a.gzipContent
	}
	header.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(body)
}

// etagMatches implements the If-None-Match comparison rules for a single
// strong validator, accepting multiple or weak validators from clients.
func etagMatches(header, etag string) bool {
	if header == "" {
		return false
	}
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" {
			return true
		}
		candidate = strings.TrimPrefix(candidate, "W/")
		if candidate == etag {
			return true
		}
	}
	return false
}

// acceptsGzip reports whether the Accept-Encoding header allows gzip,
// honoring explicit quality values such as "gzip;q=0".
func acceptsGzip(header string) bool {
	for _, part := range strings.Split(header, ",") {
		fields := strings.Split(strings.TrimSpace(part), ";")
		encoding := strings.ToLower(strings.TrimSpace(fields[0]))
		if encoding != "gzip" && encoding != "*" {
			continue
		}
		quality := 1.0
		for _, field := range fields[1:] {
			field = strings.TrimSpace(field)
			if !strings.HasPrefix(field, "q=") {
				continue
			}
			if parsed, err := strconv.ParseFloat(strings.TrimPrefix(field, "q="), 64); err == nil {
				quality = parsed
			}
		}
		if quality > 0 {
			return true
		}
	}
	return false
}
