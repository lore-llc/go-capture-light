package main

import (
	"bufio"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// SegmentWatcher polls the ffmpeg segment directory for new .ts files,
// reads them, sends them to the server, and cleans up.
type SegmentWatcher struct {
	client       *Client
	sessionID    string
	segDir       string
	inputTracker *InputTracker

	segmentIndex int
	startedAt    time.Time       // wall-clock time when ffmpeg started capturing
	segListPath  string          // path to ffmpeg's segment list CSV
	segTimes     map[string]float64 // filename → start time in seconds (from CSV)
	sent         map[string]bool // track already-sent filenames

	stopCh chan struct{}
	doneCh chan struct{}
}

// NewSegmentWatcher creates a new watcher for the given segment directory.
func NewSegmentWatcher(client *Client, segDir, sessionID, segListPath string, startedAt time.Time, inputTracker *InputTracker) *SegmentWatcher {
	return &SegmentWatcher{
		client:       client,
		sessionID:    sessionID,
		segDir:       segDir,
		inputTracker: inputTracker,
		startedAt:    startedAt,
		segListPath:  segListPath,
		segTimes:     make(map[string]float64),
		sent:         make(map[string]bool),
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
	}
}

// Start begins polling for segments. Blocks until Stop is called.
func (sw *SegmentWatcher) Start() {
	log.Printf("Segment watcher started (dir=%s, session=%s)", sw.segDir, sw.sessionID)

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-sw.stopCh:
			// Final flush on shutdown
			sw.scanAndSend()
			close(sw.doneCh)
			return
		case <-ticker.C:
			sw.scanAndSend()
		}
	}
}

// Stop signals the watcher to do a final flush and exit.
func (sw *SegmentWatcher) Stop() {
	close(sw.stopCh)
	<-sw.doneCh
}

// FlushRemaining sends any .ts files still in the directory.
// Called after ffmpeg has been stopped to pick up the final segment.
func (sw *SegmentWatcher) FlushRemaining() {
	// Give ffmpeg a moment to finalize the last segment and update the CSV
	time.Sleep(200 * time.Millisecond)
	sw.scanAndSend()
}

// refreshSegList re-reads ffmpeg's segment list CSV to get actual segment start times.
// CSV format: segment_00000.ts,0.000000,3.250000
//
//	(filename, start_seconds, end_seconds)
func (sw *SegmentWatcher) refreshSegList() {
	f, err := os.Open(sw.segListPath)
	if err != nil {
		return // file may not exist yet
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ",", 3)
		if len(parts) < 2 {
			continue
		}
		filename := parts[0]
		if _, already := sw.segTimes[filename]; already {
			continue
		}
		startSec, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			continue
		}
		sw.segTimes[filename] = startSec
	}
}

// segmentTimestamp returns the wall-clock time when a segment started capturing.
// Uses ffmpeg's CSV segment list for the real start offset, falling back to
// index * estimated duration if the CSV entry isn't available yet.
func (sw *SegmentWatcher) segmentTimestamp(filename string) time.Time {
	if startSec, ok := sw.segTimes[filename]; ok {
		return sw.startedAt.Add(time.Duration(startSec * float64(time.Second)))
	}
	// Fallback: estimate from index (less accurate but never blocks)
	return sw.startedAt.Add(time.Duration(sw.segmentIndex*3) * time.Second)
}

// scanAndSend looks for new .ts files and sends them.
func (sw *SegmentWatcher) scanAndSend() {
	// Re-read the segment list CSV for accurate timestamps
	sw.refreshSegList()

	entries, err := os.ReadDir(sw.segDir)
	if err != nil {
		return
	}

	// Collect and sort .ts files by name (segment_%05d.ts sorts chronologically)
	var tsFiles []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".ts") && !sw.sent[e.Name()] {
			tsFiles = append(tsFiles, e.Name())
		}
	}
	sort.Strings(tsFiles)

	// Send all but the last file — the last one might still be written by ffmpeg.
	// Exception: if stop has been signaled, send everything.
	sendAll := sw.isStopping()
	count := len(tsFiles)
	if !sendAll && count > 0 {
		count = count - 1 // skip the most recent (possibly in-progress) file
	}

	for i := 0; i < count; i++ {
		name := tsFiles[i]
		path := filepath.Join(sw.segDir, name)

		data, err := os.ReadFile(path)
		if err != nil {
			log.Printf("Segment read error (%s): %v", name, err)
			continue
		}

		if len(data) == 0 {
			continue
		}

		// Drain input actions to send alongside this segment
		var actions []InputAction
		if sw.inputTracker != nil {
			actions = sw.inputTracker.Drain()
		}

		segStart := sw.segmentTimestamp(name)
		if err := sw.client.SendSegment(sw.sessionID, sw.segmentIndex, data, segStart, actions); err != nil {
			log.Printf("Segment %d send failed (%s, %d bytes): %v", sw.segmentIndex, name, len(data), err)
			continue // will retry on next poll
		}

		log.Printf("Segment %d sent (%s, %d bytes, %d actions)", sw.segmentIndex, name, len(data), len(actions))
		sw.segmentIndex++
		sw.sent[name] = true

		// Clean up the sent file
		os.Remove(path)
	}
}

// isStopping checks if the stop signal has been sent.
func (sw *SegmentWatcher) isStopping() bool {
	select {
	case <-sw.stopCh:
		return true
	default:
		return false
	}
}
