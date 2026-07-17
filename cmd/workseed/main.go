package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"workseed/internal/api"
	"workseed/internal/store"
	"workseed/internal/webui"
)

func main() {
	host := flag.String("host", "127.0.0.1", "listen host")
	port := flag.Int("port", 8080, "listen port")
	dataDir := flag.String("data", "./data", "data directory")
	flag.Parse()

	if err := os.MkdirAll(*dataDir, 0o755); err != nil {
		log.Fatalf("create data directory: %v", err)
	}

	db, err := store.Open(filepath.Join(*dataDir, "workseed.db"))
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	mux := http.NewServeMux()
	api.Register(mux, db)
	mux.Handle("/", webui.Handler())

	addr := fmt.Sprintf("%s:%d", *host, *port)
	log.Printf("拾种 Workseed is running at http://%s", addr)
	if err := http.ListenAndServe(addr, logRequests(mux)); err != nil {
		log.Fatal(err)
	}
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}
