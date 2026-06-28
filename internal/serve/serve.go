// Package serve hosts the interactive dashboard. It reads ONLY the graph (which
// it serves as /graph.json) — the analyzer stays fully usable headless.
package serve

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"runtime"

	_ "embed"

	"github.com/sattamBytes/flowgraph/internal/graph"
)

//go:embed web/index.html
var indexHTML []byte

// Serve renders g at addr (e.g. "localhost:8080"). If open is true it tries to
// launch the default browser.
func Serve(g *graph.Graph, addr string, open bool) error {
	data, err := json.Marshal(g)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/graph.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexHTML)
	})
	url := "http://" + addr
	fmt.Printf("flowgraph dashboard: %s  (Ctrl-C to stop)\n", url)
	if open {
		openBrowser(url)
	}
	return http.ListenAndServe(addr, mux)
}

func openBrowser(url string) {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	case "windows":
		cmd, args = "rundll32", []string{"url.dll,FileProtocolHandler"}
	default:
		cmd = "xdg-open"
	}
	_ = exec.Command(cmd, append(args, url)...).Start()
}
