package main

import (
	"bytes"
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
	apiKey     string
	httpClient *http.Client
}

// NewClient creates a new LORE API client.
func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// StartSession creates a new session and returns the session ID.
func (c *Client) StartSession(task, name string) (string, error) {
	body := map[string]interface{}{
		"name": name,
		"task": task,
	}

	var resp struct {
		SessionID string `json:"session_id"`
	}
	if err := c.doJSON("POST", "/v1/sessions/start", body, &resp); err != nil {
		return "", err
	}
	return resp.SessionID, nil
}

// BatchPayload is the request body for the bare ingest endpoint.
type BatchPayload struct {
	BatchID        string        `json:"batch_id"`
	Frames         []FramePayload `json:"frames"`
	Actions        []interface{} `json:"actions"`
	AppContext     []interface{} `json:"app_context"`
	AXSnapshots    []interface{} `json:"ax_snapshots"`
	Clipboard      []interface{} `json:"clipboard"`
	WindowGeometry []interface{} `json:"window_geometry"`
}

// FramePayload is a single frame in a batch.
type FramePayload struct {
	ScreenshotBase64 string `json:"screenshot_base64"`
	Timestamp        string `json:"timestamp"`
}

// SendBareStreamBatch sends a micro-batch to the bare ingest endpoint.
func (c *Client) SendBareStreamBatch(sessionID string, batch BatchPayload) error {
	path := fmt.Sprintf("/v1/sessions/%s/ingest/stream", sessionID)
	return c.doJSON("POST", path, batch, nil)
}

// doJSON makes an authenticated JSON request.
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
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}

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
