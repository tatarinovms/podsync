package web

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
	"strings"
)

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
