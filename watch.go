package main

import (
	"encoding/base64"
	"fmt"
	"log"
	"sync"
	"time"
)

// CapturedFrame holds a screenshot (JPEG) and its timestamp.
type CapturedFrame struct {
	JPEGBytes []byte
	Timestamp time.Time
}

// WatcherConfig holds all configuration for the watcher.
type WatcherConfig struct {
	SessionID     string
	FPS           int
	BatchInterval time.Duration
}

// Watcher captures screenshots and streams batches.
type Watcher struct {
	client  *Client
	capture CaptureFn
	config  WatcherConfig

	// Pending frames for next batch
	pending   []CapturedFrame
	pendingMu sync.Mutex

	// Batch counter
	batchIndex int

	// Lifecycle
	stopCh chan struct{}
	doneCh chan struct{}
}

// NewWatcher creates a new screen watcher.
func NewWatcher(client *Client, capture CaptureFn, config WatcherConfig) *Watcher {
	return &Watcher{
		client:  client,
		capture: capture,
		config:  config,
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
	}
}

// Start begins capturing and streaming. Blocks until Stop is called.
func (w *Watcher) Start() {
	interval := time.Duration(1000/w.config.FPS) * time.Millisecond
	captureTicker := time.NewTicker(interval)
	batchTicker := time.NewTicker(w.config.BatchInterval)
	defer captureTicker.Stop()
	defer batchTicker.Stop()

	log.Printf("Watching (fps=%d, batch=%s, session=%s)",
		w.config.FPS, w.config.BatchInterval, w.config.SessionID)

	for {
		select {
		case <-w.stopCh:
			w.flushBatch()
			close(w.doneCh)
			return
		case <-captureTicker.C:
			w.captureFrame()
		case <-batchTicker.C:
			go w.flushBatch()
		}
	}
}

// Stop signals the watcher to flush and exit.
func (w *Watcher) Stop() {
	close(w.stopCh)
	<-w.doneCh
}

// captureFrame takes a screenshot and queues it.
func (w *Watcher) captureFrame() {
	data, err := w.capture()
	if err != nil {
		log.Printf("Capture error: %v", err)
		return
	}

	w.pendingMu.Lock()
	w.pending = append(w.pending, CapturedFrame{
		JPEGBytes: data,
		Timestamp: time.Now().UTC(),
	})
	w.pendingMu.Unlock()
}

// flushBatch drains pending frames and sends them as a micro-batch.
func (w *Watcher) flushBatch() {
	w.pendingMu.Lock()
	frames := w.pending
	w.pending = nil
	w.pendingMu.Unlock()

	if len(frames) == 0 {
		return
	}

	var framePayloads []FramePayload
	for _, f := range frames {
		framePayloads = append(framePayloads, FramePayload{
			ScreenshotBase64: base64.StdEncoding.EncodeToString(f.JPEGBytes),
			Timestamp:        f.Timestamp.Format(time.RFC3339Nano),
		})
	}

	batch := BatchPayload{
		BatchID:        fmt.Sprintf("batch-%d", w.batchIndex),
		Frames:         framePayloads,
		Actions:        []interface{}{},
		AppContext:     []interface{}{},
		AXSnapshots:    []interface{}{},
		Clipboard:      []interface{}{},
		WindowGeometry: []interface{}{},
	}
	w.batchIndex++

	if err := w.client.SendBareStreamBatch(w.config.SessionID, batch); err != nil {
		log.Printf("Batch %s failed: %v", batch.BatchID, err)
	} else {
		log.Printf("Batch %s: %d frames sent", batch.BatchID, len(framePayloads))
	}
}
