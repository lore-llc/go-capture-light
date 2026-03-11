package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is a thin HTTP client for the LORE API.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new LORE API client.
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// StartSession creates a new session and returns the session ID.
func (c *Client) StartSession(task, name, userID string) (string, error) {
	body := map[string]interface{}{
		"name": name,
		"task": task,
	}
	if userID != "" {
		body["user_id"] = userID
	}

	var resp struct {
		SessionID string `json:"session_id"`
	}
	if err := c.doJSON("POST", "/v1/sessions/start", body, &resp); err != nil {
		return "", err
	}
	return resp.SessionID, nil
}

// SegmentBatchPayload is the request body sent to the existing ingest/stream endpoint.
// Matches the old BatchPayload shape but with frames=[] and a new video_segment field.
type SegmentBatchPayload struct {
	BatchID        string        `json:"batch_id"`
	Frames         []interface{} `json:"frames"`
	Actions        []interface{} `json:"actions"`
	AppContext     []interface{} `json:"app_context"`
	AXSnapshots    []interface{} `json:"ax_snapshots"`
	Clipboard      []interface{} `json:"clipboard"`
	WindowGeometry []interface{} `json:"window_geometry"`
	VideoSegment   *VideoSegment `json:"video_segment,omitempty"`
}

// VideoSegment holds the H.264 .ts segment data sent alongside the batch.
type VideoSegment struct {
	Format      string `json:"format"`       // "mpegts"
	Codec       string `json:"codec"`        // "h264"
	DurationSec int    `json:"duration_sec"` // segment duration (3)
	Index       int    `json:"index"`        // segment sequence number
	Timestamp   string `json:"timestamp"`    // RFC3339Nano
	DataBase64  string `json:"data_base64"`  // base64-encoded .ts bytes
}

// actionToServerFormat converts an InputAction to the server's StreamAction shape:
// { "type": "...", "timestamp": "...", "metadata": { "x": ..., "y": ..., "key": ..., "modifiers": [...] } }
func actionToServerFormat(a InputAction) map[string]interface{} {
	doc := map[string]interface{}{
		"type":      a.Type,
		"timestamp": a.Timestamp,
	}
	if a.X != 0 || a.Y != 0 {
		doc["x"] = a.X
		doc["y"] = a.Y
	}
	if a.Key != "" {
		doc["key"] = a.Key
	}
	if len(a.Modifiers) > 0 {
		doc["modifiers"] = a.Modifiers
	}
	return doc
}

// SendSegment sends a .ts segment to the existing ingest/stream endpoint.
func (c *Client) SendSegment(sessionID string, segmentIndex int, tsData []byte, timestamp time.Time, actions []InputAction) error {
	// Convert actions to server format
	var actionDocs []interface{}
	if len(actions) > 0 {
		actionDocs = make([]interface{}, len(actions))
		for i, a := range actions {
			actionDocs[i] = actionToServerFormat(a)
		}
	} else {
		actionDocs = []interface{}{}
	}

	batch := SegmentBatchPayload{
		BatchID:        fmt.Sprintf("segment-%d", segmentIndex),
		Frames:         []interface{}{},
		Actions:        actionDocs,
		AppContext:     []interface{}{},
		AXSnapshots:    []interface{}{},
		Clipboard:      []interface{}{},
		WindowGeometry: []interface{}{},
		VideoSegment: &VideoSegment{
			Format:      "mpegts",
			Codec:       "h264",
			DurationSec: 3,
			Index:       segmentIndex,
			Timestamp:   timestamp.Format(time.RFC3339Nano),
			DataBase64:  base64.StdEncoding.EncodeToString(tsData),
		},
	}

	path := fmt.Sprintf("/v1/sessions/%s/ingest/stream", sessionID)
	return c.doJSON("POST", path, batch, nil)
}

// doJSON makes a JSON request.
func (c *Client) doJSON(method, path string, body interface{}, result interface{}) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, c.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	if result != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("unmarshal: %w", err)
		}
	}
	return nil
}
