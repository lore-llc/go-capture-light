package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
)

var version = "dev"

func main() {
	// Load .env before parsing flags so env vars are available as defaults
	loadDotEnv(".env")
	loadDotEnv(".env.local")

	showVersion := flag.Bool("version", false, "Print version and exit")
	apiURL := flag.String("api-url", envOrDefault("LORE_API_URL", "https://lore-agent-memory.onrender.com"), "LORE API base URL")
	task := flag.String("task", "", "Session task description")
	name := flag.String("name", "", "Session name")
	userID := flag.String("user-id", envOrDefault("LORE_USER_ID", ""), "User ID for multi-user tracking (optional)")
	fps := flag.Int("fps", 4, "Capture frames per second")
	resolution := flag.String("resolution", "720p", "Screenshot resolution: 720p (1280px) or 1080p (1920px)")
	flag.Parse()

	if *showVersion {
		fmt.Println("lore-watch-light", version)
		os.Exit(0)
	}

	var maxWidth int
	switch *resolution {
	case "720p", "720":
		maxWidth = 1280
	case "1080p", "1080":
		maxWidth = 1920
	default:
		log.Fatalf("Invalid --resolution %q: must be 720p or 1080p", *resolution)
	}

	// Require ffmpeg
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		log.Fatal("ffmpeg not found — install ffmpeg to use lore-watch-light")
	}

	client := NewClient(*apiURL)

	// Start session
	sessionID, err := client.StartSession(*task, *name, *userID)
	if err != nil {
		log.Fatalf("Failed to start session: %v", err)
	}
	fmt.Printf("session_id=%s\n", sessionID)

	// Start ffmpeg capture
	capture, err := NewFFmpegCapture(FFmpegConfig{
		FFmpegPath: ffmpegPath,
		SessionID:  sessionID,
		FPS:        *fps,
		MaxWidth:   maxWidth,
		SegmentSec: 3,
	})
	if err != nil {
		log.Fatalf("Failed to initialize ffmpeg capture: %v", err)
	}
	defer capture.Cleanup()

	if err := capture.Start(); err != nil {
		log.Fatalf("Failed to start ffmpeg: %v", err)
	}

	// Start input tracker (xinput on Linux, no-op on macOS)
	inputTracker := NewInputTracker()
	if err := inputTracker.Start(); err != nil {
		log.Printf("Warning: input tracking failed to start: %v", err)
	}

	// Start segment watcher
	segWatcher := NewSegmentWatcher(client, capture.SegmentDir(), sessionID, capture.segListPath, capture.StartedAt, inputTracker)

	// Handle signals for clean shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		fmt.Println("\nShutting down...")
		inputTracker.Stop()
		capture.Stop()
		segWatcher.FlushRemaining()
		segWatcher.Stop()
	}()

	segWatcher.Start()
	fmt.Println("Done.")
}

// envOrDefault returns the environment variable value or a default.
func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// loadDotEnv reads a .env file and sets environment variables (won't override existing).
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return // file not found, skip silently
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		// Remove surrounding quotes
		v = strings.Trim(v, `"'`)
		// Don't override existing env vars
		if os.Getenv(k) == "" {
			os.Setenv(k, v)
		}
	}
}
