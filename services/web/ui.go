package web

import (
	"embed"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"strings"
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

func (s *Server) adminUIHandler() http.Handler {
	sub, err := fs.Sub(adminContent, "ui")
	if err != nil {
		panic(err)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/admin")
		path = strings.TrimPrefix(path, "/")
		if path == "" || path == "login" || path == "index.html" {
			f, err := sub.Open("index.html")
			if err != nil {
				http.NotFound(w, r)
				return
			}
			defer f.Close()
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.Copy(w, f)
			return
		}

		// Try to open static asset
		f, err := sub.Open(path)
		if err == nil {
			defer f.Close()
			stat, _ := f.Stat()
			if stat != nil && !stat.IsDir() {
				if seeker, ok := f.(io.ReadSeeker); ok {
					if strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".css") || strings.HasSuffix(path, ".woff2") || strings.HasSuffix(path, ".woff") {
						w.Header().Set("Cache-Control", "public, max-age=604800")
					}
					http.ServeContent(w, r, path, stat.ModTime(), seeker)
					return
				}
				w.Header().Set("Content-Type", "application/octet-stream")
				_, _ = io.Copy(w, f)
				return
			}
		}

		// Fallback to index.html for SPA routes
		f, err = sub.Open("index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.Copy(w, f)
	})
}
