package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// CaptureFn takes a screenshot and returns JPEG bytes.
type CaptureFn func() ([]byte, error)

// DetectCaptureFn auto-detects the best screenshot tool for the current platform.
func DetectCaptureFn() (CaptureFn, error) {
	switch runtime.GOOS {
	case "darwin":
		return captureMacOS, nil
	case "linux":
		if _, err := exec.LookPath("grim"); err == nil {
			return captureGrim, nil
		}
		if _, err := exec.LookPath("import"); err == nil {
			return captureImport, nil
		}
		if _, err := exec.LookPath("scrot"); err == nil {
			return captureScrot, nil
		}
		return nil, fmt.Errorf("no screenshot tool found (install grim, imagemagick, or scrot)")
	default:
		return nil, fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
}

// captureMacOS uses the built-in screencapture command, outputting JPEG directly.
func captureMacOS() ([]byte, error) {
	tmpFile := filepath.Join(os.TempDir(), "lore_capture.jpg")
	cmd := exec.Command("screencapture", "-x", "-t", "jpg", tmpFile)
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("screencapture: %w", err)
	}
	data, err := os.ReadFile(tmpFile)
	if err != nil {
		return nil, fmt.Errorf("read screenshot: %w", err)
	}
	return data, nil
}

// captureGrim uses grim (Wayland), outputting JPEG directly to stdout.
func captureGrim() ([]byte, error) {
	var buf bytes.Buffer
	cmd := exec.Command("grim", "-t", "jpeg", "-q", "80", "-")
	cmd.Stdout = &buf
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("grim: %w", err)
	}
	return buf.Bytes(), nil
}

// captureImport uses ImageMagick import (X11), outputting JPEG directly to stdout.
func captureImport() ([]byte, error) {
	var buf bytes.Buffer
	cmd := exec.Command("import", "-window", "root", "jpg:-")
	cmd.Stdout = &buf
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("import: %w", err)
	}
	return buf.Bytes(), nil
}

// captureScrot uses scrot (X11 fallback), outputting JPEG directly.
func captureScrot() ([]byte, error) {
	tmpFile := filepath.Join(os.TempDir(), "lore_capture.jpg")
	cmd := exec.Command("scrot", "-o", "-q", "80", tmpFile)
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("scrot: %w", err)
	}
	data, err := os.ReadFile(tmpFile)
	if err != nil {
		return nil, fmt.Errorf("read screenshot: %w", err)
	}
	return data, nil
}
