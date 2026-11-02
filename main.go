// Tonttivahti watches the City of Oulu's plot service and sends ntfy alerts for new matching plots.
package main

import (
	"context"
	"embed"
	"flag"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

const port = "47321"

//go:embed all:web/dist
var webDist embed.FS

func main() {
	background := flag.Bool("background", false, "start without opening the browser (used at login)")
	flag.Parse()
	appURL := "http://127.0.0.1:" + port + "/"

	dataDir, err := appDataDir()
	if err != nil {
		log.Fatal(err)
	}
	logFile, err := os.OpenFile(filepath.Join(dataDir, "tonttivahti.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err == nil {
		log.SetOutput(logFile) // the Windows build has no console
	}

	listener, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		// shortcut: assumes whatever holds the port is our own running instance; a port clash would open the wrong page.
		log.Printf("already running (%v); opening the browser", err)
		openBrowser(appURL)
		return
	}

	store, err := OpenStore(filepath.Join(dataDir, "profiles.json"))
	if err != nil {
		log.Fatalf("opening profiles: %v", err)
	}
	watcher := NewWatcher(store)
	go watcher.Run(context.Background())

	static, err := fs.Sub(webDist, "web/dist")
	if err != nil {
		log.Fatal(err)
	}
	if !*background {
		openBrowser(appURL)
	}
	srv := &http.Server{Handler: newServer(store, watcher, port, static), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: time.Minute}
	log.Fatal(srv.Serve(listener))
}

func appDataDir() (string, error) {
	base, err := os.UserConfigDir() // %APPDATA% on Windows
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "Tonttivahti")
	return dir, os.MkdirAll(dir, 0o700)
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		log.Printf("opening browser: %v", err)
	}
}
