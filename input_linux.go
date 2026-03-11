//go:build linux

package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// InputAction represents a single mouse or keyboard event.
type InputAction struct {
	Type      string   `json:"type"`      // "click", "keypress", "scroll", "move", "drag"
	Timestamp string   `json:"timestamp"` // RFC3339Nano
	X         float64  `json:"x,omitempty"`
	Y         float64  `json:"y,omitempty"`
	Key       string   `json:"key,omitempty"`
	Modifiers []string `json:"modifiers,omitempty"`
}

// InputTracker captures mouse and keyboard events via xinput on Linux.
type InputTracker struct {
	mu      sync.Mutex
	actions []InputAction

	cmd    *exec.Cmd
	stopCh chan struct{}
	doneCh chan struct{}

	// State machine
	buttonHeld   bool
	lastMoveTime time.Time
}

// NewInputTracker creates a new InputTracker.
func NewInputTracker() *InputTracker {
	return &InputTracker{
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
}

// Start launches the xinput subprocess and begins parsing events.
func (t *InputTracker) Start() error {
	display := os.Getenv("DISPLAY")
	if display == "" {
		display = ":99"
		os.Setenv("DISPLAY", display)
	}

	xinputPath, err := exec.LookPath("xinput")
	if err != nil {
		close(t.doneCh)
		return fmt.Errorf("xinput not found — install xinput for input tracking")
	}

	t.cmd = exec.Command(xinputPath, "test-xi2", "--root")
	t.cmd.Env = append(os.Environ(), "DISPLAY="+display)

	stdout, err := t.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("xinput stdout pipe: %w", err)
	}

	if err := t.cmd.Start(); err != nil {
		return fmt.Errorf("xinput start: %w", err)
	}

	log.Printf("Input tracker started (xinput pid=%d, DISPLAY=%s)", t.cmd.Process.Pid, display)

	go func() {
		defer close(t.doneCh)
		scanner := bufio.NewScanner(stdout)

		var (
			eventType string // "motion", "button", "button_release", "key", or ""
			detail    int
			rootX     float64
			rootY     float64
		)

		emitPrevious := func() {
			if eventType == "" {
				return
			}
			now := time.Now().UTC().Format(time.RFC3339Nano)

			switch eventType {
			case "button":
				if detail >= 1 && detail <= 3 {
					// Click
					t.appendAction(InputAction{
						Type:      "click",
						Timestamp: now,
						X:         rootX,
						Y:         rootY,
					})
					t.mu.Lock()
					t.buttonHeld = true
					t.mu.Unlock()
				} else if detail == 4 || detail == 5 {
					// Scroll: 4=up, 5=down
					t.appendAction(InputAction{
						Type:      "scroll",
						Timestamp: now,
						X:         rootX,
						Y:         rootY,
						Key:       scrollDirection(detail),
					})
				}

			case "button_release":
				t.mu.Lock()
				t.buttonHeld = false
				t.mu.Unlock()

			case "motion":
				throttle := t.currentThrottle()
				t.mu.Lock()
				sinceLastMove := time.Since(t.lastMoveTime)
				held := t.buttonHeld
				t.mu.Unlock()

				if sinceLastMove < throttle {
					break
				}

				actionType := "move"
				if held {
					actionType = "drag"
				}
				t.appendAction(InputAction{
					Type:      actionType,
					Timestamp: now,
					X:         rootX,
					Y:         rootY,
				})
				t.mu.Lock()
				t.lastMoveTime = time.Now()
				t.mu.Unlock()

			case "key":
				keyName := keycodeToName(detail)
				t.appendAction(InputAction{
					Type:      "keypress",
					Timestamp: now,
					Key:       keyName,
				})
			}
		}

		for scanner.Scan() {
			select {
			case <-t.stopCh:
				return
			default:
			}

			line := strings.TrimSpace(scanner.Text())

			if strings.HasPrefix(line, "EVENT type") {
				emitPrevious()
				eventType = ""
				detail = 0
				rootX = 0
				rootY = 0

				switch {
				case strings.Contains(line, "(RawMotion)"),
					strings.Contains(line, "(RawButtonPress)"),
					strings.Contains(line, "(RawButtonRelease)"),
					strings.Contains(line, "(RawKeyPress)"),
					strings.Contains(line, "(RawKeyRelease)"):
					// Skip raw events
				case strings.Contains(line, "(ButtonPress)"):
					eventType = "button"
				case strings.Contains(line, "(ButtonRelease)"):
					eventType = "button_release"
				case strings.Contains(line, "(Motion)"):
					eventType = "motion"
				case strings.Contains(line, "(KeyPress)"):
					eventType = "key"
				}
				continue
			}

			if eventType == "" {
				continue
			}

			if strings.HasPrefix(line, "detail:") {
				fmt.Sscanf(line, "detail: %d", &detail)
			}
			if strings.HasPrefix(line, "root:") {
				fmt.Sscanf(line, "root: %f/%f", &rootX, &rootY)
			}
		}

		// Emit final event
		emitPrevious()
	}()

	return nil
}

// currentThrottle returns the current motion throttle based on buffer size.
// Adaptive: 20ms normally, 50ms when buffer > 400, 100ms when buffer > 600.
func (t *InputTracker) currentThrottle() time.Duration {
	t.mu.Lock()
	n := len(t.actions)
	t.mu.Unlock()

	switch {
	case n > 600:
		return 100 * time.Millisecond
	case n > 400:
		return 50 * time.Millisecond
	default:
		return 20 * time.Millisecond
	}
}

// appendAction adds an action to the buffer (thread-safe).
func (t *InputTracker) appendAction(a InputAction) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.actions = append(t.actions, a)
}

// Drain returns all buffered actions and resets the buffer.
// Applies overflow reduction if over 500 actions.
func (t *InputTracker) Drain() []InputAction {
	t.mu.Lock()
	actions := t.actions
	t.actions = nil
	t.mu.Unlock()

	if len(actions) <= 500 {
		return actions
	}

	return reduceActions(actions, 500)
}

// reduceActions applies graceful degradation to bring action count under maxActions.
// Priority: clicks/keypress/scroll/drag are never dropped; moves are downsampled first.
func reduceActions(actions []InputAction, maxActions int) []InputAction {
	// Separate high-priority (clicks, keys, scrolls, drags) from moves
	var highPriority []InputAction
	var moves []InputAction
	for _, a := range actions {
		if a.Type == "move" {
			moves = append(moves, a)
		} else {
			highPriority = append(highPriority, a)
		}
	}

	// If high-priority alone exceeds cap, keep first maxActions chronologically
	if len(highPriority) >= maxActions {
		return highPriority[:maxActions]
	}

	// Budget remaining for moves
	moveBudget := maxActions - len(highPriority)
	if moveBudget >= len(moves) {
		// Enough room — no reduction needed after all
		return actions
	}

	// Downsample moves: keep every Nth to fit budget
	var sampledMoves []InputAction
	if moveBudget > 0 {
		step := len(moves) / moveBudget
		if step < 1 {
			step = 1
		}
		for i := 0; i < len(moves) && len(sampledMoves) < moveBudget; i += step {
			sampledMoves = append(sampledMoves, moves[i])
		}
	}

	// Merge back in chronological order
	result := make([]InputAction, 0, len(highPriority)+len(sampledMoves))
	mi := 0
	si := 0
	// Walk through original order, keeping high-priority and sampled moves
	sampledSet := make(map[int]bool, len(sampledMoves))
	// Map sampled moves back to their original indices in the moves slice
	if len(sampledMoves) > 0 {
		step := len(moves) / moveBudget
		if step < 1 {
			step = 1
		}
		for i := 0; i < len(moves) && si < len(sampledMoves); i += step {
			sampledSet[i] = true
			si++
		}
	}

	mi = 0 // index into moves slice
	for _, a := range actions {
		if a.Type == "move" {
			if sampledSet[mi] {
				result = append(result, a)
			}
			mi++
		} else {
			result = append(result, a)
		}
	}

	return result
}

// Stop terminates the xinput subprocess.
func (t *InputTracker) Stop() {
	close(t.stopCh)
	if t.cmd != nil && t.cmd.Process != nil {
		t.cmd.Process.Kill()
	}
	<-t.doneCh
	log.Printf("Input tracker stopped")
}

func scrollDirection(detail int) string {
	if detail == 4 {
		return "up"
	}
	return "down"
}

// keycodeToName maps X11 keycodes to human-readable key names.
func keycodeToName(code int) string {
	if name, ok := x11KeycodeMap[code]; ok {
		return name
	}
	return fmt.Sprintf("key_%d", code)
}

// x11KeycodeMap maps common X11 keycodes to key names.
// Based on standard US QWERTY layout (xmodmap -pke).
var x11KeycodeMap = map[int]string{
	// Number row
	10: "1", 11: "2", 12: "3", 13: "4", 14: "5",
	15: "6", 16: "7", 17: "8", 18: "9", 19: "0",

	// Top row (QWERTY)
	24: "q", 25: "w", 26: "e", 27: "r", 28: "t",
	29: "y", 30: "u", 31: "i", 32: "o", 33: "p",

	// Home row (ASDF)
	38: "a", 39: "s", 40: "d", 41: "f", 42: "g",
	43: "h", 44: "j", 45: "k", 46: "l",

	// Bottom row (ZXCV)
	52: "z", 53: "x", 54: "c", 55: "v", 56: "b",
	57: "n", 58: "m",

	// Special keys
	9:   "Escape",
	22:  "BackSpace",
	23:  "Tab",
	36:  "Return",
	65:  "space",
	66:  "Caps_Lock",
	119: "Delete",

	// Modifiers
	37:  "Control_L",
	50:  "Shift_L",
	62:  "Shift_R",
	64:  "Alt_L",
	108: "Alt_R",
	105: "Control_R",
	133: "Super_L",
	134: "Super_R",

	// Arrow keys
	111: "Up",
	113: "Left",
	114: "Right",
	116: "Down",

	// Function keys
	67: "F1", 68: "F2", 69: "F3", 70: "F4",
	71: "F5", 72: "F6", 73: "F7", 74: "F8",
	75: "F9", 76: "F10", 95: "F11", 96: "F12",

	// Navigation
	110: "Home",
	112: "Prior", // Page Up
	115: "End",
	117: "Next", // Page Down
	118: "Insert",

	// Punctuation & symbols
	20: "minus",
	21: "equal",
	34: "bracketleft",
	35: "bracketright",
	47: "semicolon",
	48: "apostrophe",
	49: "grave",
	51: "backslash",
	59: "comma",
	60: "period",
	61: "slash",

	// Numpad
	79: "KP_7", 80: "KP_8", 81: "KP_9",
	83: "KP_4", 84: "KP_5", 85: "KP_6",
	87: "KP_1", 88: "KP_2", 89: "KP_3",
	90:  "KP_0",
	82:  "KP_Subtract",
	86:  "KP_Add",
	91:  "KP_Decimal",
	104: "KP_Enter",
	106: "KP_Divide",
	63:  "KP_Multiply",

	// Misc
	78:  "Scroll_Lock",
	107: "Print",
	127: "Pause",
	77:  "Num_Lock",
}
