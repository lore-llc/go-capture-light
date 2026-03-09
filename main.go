package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

var version = "dev"

func main() {
	// Load .env before parsing flags so env vars are available as defaults
	loadDotEnv(".env")
	loadDotEnv(".env.local")

	showVersion := flag.Bool("version", false, "Print version and exit")
	apiKey := flag.String("api-key", envOrDefault("LORE_API_KEY", ""), "API key for authentication")
	apiURL := flag.String("api-url", envOrDefault("LORE_API_URL", "https://lore-agent-memory.onrender.com"), "LORE API base URL")
	task := flag.String("task", "", "Session task description")
	name := flag.String("name", "", "Session name")
	userID := flag.String("user-id", envOrDefault("LORE_USER_ID", ""), "User ID for multi-user tracking (optional)")
	fps := flag.Int("fps", 5, "Capture frames per second")
	batchInterval := flag.Duration("batch-interval", 3*time.Second, "Interval between batch flushes")
	flag.Parse()

	if *showVersion {
		fmt.Println("lore-watch-light", version)
		os.Exit(0)
	}

	if *apiKey == "" {
		log.Fatal("API key required: set LORE_API_KEY or pass --api-key")
	}

	client := NewClient(*apiURL, *apiKey)

	// Start session
	sessionID, err := client.StartSession(*task, *name, *userID)
	if err != nil {
		log.Fatalf("Failed to start session: %v", err)
	}
	fmt.Printf("session_id=%s\n", sessionID)

	// Detect screenshot tool
	captureFn, err := DetectCaptureFn()
	if err != nil {
		log.Fatalf("No screenshot tool found: %v", err)
	}

	// Set up watcher
	watcher := NewWatcher(client, captureFn, WatcherConfig{
		SessionID:     sessionID,
		FPS:           *fps,
		BatchInterval: *batchInterval,
	})

	// Handle signals for clean shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		fmt.Println("\nShutting down...")
		watcher.Stop()
	}()

	// Block until stopped
	watcher.Start()
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
