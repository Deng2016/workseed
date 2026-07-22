package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"workseed/internal/api"
	"workseed/internal/store"
	"workseed/internal/webui"
)

const (
	defaultHost = "127.0.0.1"
	firstPort   = 8866
	dataDir     = "./data"
)

func main() {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		log.Fatalf("create data directory: %v", err)
	}

	db, err := store.Open(filepath.Join(dataDir, "workseed.db"))
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	mux := http.NewServeMux()
	api.Register(mux, db)
	mux.Handle("/", webui.Handler())

	listener, port, err := listenOnAvailablePort(defaultHost, firstPort)
	if err != nil {
		log.Fatal(err)
	}
	defer listener.Close()

	url := fmt.Sprintf("http://%s:%d", defaultHost, port)
	log.Printf("拾种 Workseed is running at %s", url)
	go openBrowser(url)

	if err := http.Serve(listener, logRequests(mux)); err != nil {
		log.Fatal(err)
	}
}

func listenOnAvailablePort(host string, startPort int) (net.Listener, int, error) {
	for port := startPort; port <= 65535; port++ {
		listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", host, port))
		if err == nil {
			return listener, port, nil
		}
	}
	return nil, 0, fmt.Errorf("no available port found from %d to 65535", startPort)
}

func openBrowser(url string) {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		command = exec.Command("open", url)
	default:
		if runningInWSL() {
			command = exec.Command("cmd.exe", "/c", "start", "", url)
		} else {
			command = exec.Command("xdg-open", url)
		}
	}
	if err := command.Run(); err != nil {
		log.Printf("无法自动打开浏览器，请手动访问 %s: %v", url, err)
	}
}

func runningInWSL() bool {
	content, err := os.ReadFile("/proc/sys/kernel/osrelease")
	return err == nil && strings.Contains(strings.ToLower(string(content)), "microsoft")
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}
