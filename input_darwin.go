//go:build darwin

package main

// InputAction represents a single mouse or keyboard event.
type InputAction struct {
	Type      string   `json:"type"`
	Timestamp string   `json:"timestamp"`
	X         float64  `json:"x,omitempty"`
	Y         float64  `json:"y,omitempty"`
	Key       string   `json:"key,omitempty"`
	Modifiers []string `json:"modifiers,omitempty"`
}

// InputTracker is a no-op stub on macOS (the Swift client handles input tracking).
type InputTracker struct{}

func NewInputTracker() *InputTracker          { return &InputTracker{} }
func (t *InputTracker) Start() error          { return nil }
func (t *InputTracker) Drain() []InputAction  { return nil }
func (t *InputTracker) Stop()                 {}
