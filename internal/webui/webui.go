package webui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed dist
var content embed.FS

func Handler() http.Handler {
	dist, err := fs.Sub(content, "dist")
	if err != nil {
		panic(err)
	}
	files := http.FileServer(http.FS(dist))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			if _, err := fs.Stat(dist, r.URL.Path[1:]); err != nil {
				r.URL.Path = "/"
			}
		}
		files.ServeHTTP(w, r)
	})
}
