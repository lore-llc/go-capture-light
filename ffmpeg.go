package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"syscall"
	"time"
)

// FFmpegConfig holds configuration for the ffmpeg capture process.
type FFmpegConfig struct {
	FFmpegPath string
	SessionID  string
	FPS        int
	MaxWidth   int
	SegmentSec int // segment duration in seconds (default 3)
}

// FFmpegCapture manages a persistent ffmpeg subprocess that captures
// the screen and outputs self-contained H.264 .ts segments.
type FFmpegCapture struct {
	cmd         *exec.Cmd
	segmentDir  string
	segListPath string    // path to the CSV segment list written by ffmpeg
	StartedAt   time.Time // wall-clock time when ffmpeg started capturing
	config      FFmpegConfig
}

// NewFFmpegCapture creates a new FFmpegCapture (does not start it).
func NewFFmpegCapture(config FFmpegConfig) (*FFmpegCapture, error) {
	if config.SegmentSec <= 0 {
		config.SegmentSec = 3
	}
	if config.MaxWidth <= 0 {
		config.MaxWidth = 1280
	}

	segmentDir := filepath.Join(os.TempDir(), fmt.Sprintf("lore_%s", config.SessionID))
	if err := os.MkdirAll(segmentDir, 0700); err != nil {
		return nil, fmt.Errorf("create segment dir: %w", err)
	}

	return &FFmpegCapture{
		segmentDir:  segmentDir,
		segListPath: filepath.Join(segmentDir, "segments.csv"),
		config:      config,
	}, nil
}

// SegmentDir returns the directory where ffmpeg writes .ts segment files.
func (f *FFmpegCapture) SegmentDir() string {
	return f.segmentDir
}

// Start spawns the ffmpeg process. Returns an error if ffmpeg fails to start.
func (f *FFmpegCapture) Start() error {
	args, err := f.buildArgs()
	if err != nil {
		return err
	}

	log.Printf("Starting ffmpeg: %s %v", f.config.FFmpegPath, args)

	f.cmd = exec.Command(f.config.FFmpegPath, args...)
	f.cmd.Stderr = os.Stderr // let ffmpeg log to stderr for debugging

	f.StartedAt = time.Now().UTC()
	if err := f.cmd.Start(); err != nil {
		return fmt.Errorf("ffmpeg start: %w", err)
	}

	// Monitor ffmpeg in background — log if it exits unexpectedly
	go func() {
		if err := f.cmd.Wait(); err != nil {
			// SIGINT exit is expected during shutdown
			if exitErr, ok := err.(*exec.ExitError); ok {
				if exitErr.ExitCode() == 255 || exitErr.ExitCode() == -1 {
					return // normal ffmpeg exit on SIGINT
				}
			}
			log.Printf("ffmpeg exited unexpectedly: %v", err)
		}
	}()

	// Give ffmpeg a moment to initialize and verify it's running
	time.Sleep(500 * time.Millisecond)
	if f.cmd.ProcessState != nil {
		return fmt.Errorf("ffmpeg exited immediately — check stderr for errors")
	}

	log.Printf("ffmpeg started (pid=%d, segments → %s)", f.cmd.Process.Pid, f.segmentDir)
	return nil
}

// Stop sends SIGINT to ffmpeg so it finalizes the current segment, then waits.
func (f *FFmpegCapture) Stop() {
	if f.cmd == nil || f.cmd.Process == nil {
		return
	}

	log.Println("Stopping ffmpeg...")
	_ = f.cmd.Process.Signal(syscall.SIGINT)

	// Wait up to 5 seconds for ffmpeg to finalize and exit
	done := make(chan struct{})
	go func() {
		// cmd.Wait() may have already been called by the goroutine in Start()
		// so we just wait for the process state to be set
		for i := 0; i < 50; i++ {
			if f.cmd.ProcessState != nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		close(done)
	}()

	select {
	case <-done:
		log.Println("ffmpeg stopped cleanly")
	case <-time.After(5 * time.Second):
		log.Println("ffmpeg did not exit in time, killing...")
		_ = f.cmd.Process.Kill()
	}
}

// Cleanup removes the temporary segment directory.
func (f *FFmpegCapture) Cleanup() {
	if f.segmentDir != "" {
		os.RemoveAll(f.segmentDir)
	}
}

// detectDarwinScreenDevice runs ffmpeg -list_devices and finds the first
// "Capture screen" device index. Returns "0" as fallback.
func detectDarwinScreenDevice(ffmpegPath string) string {
	cmd := exec.Command(ffmpegPath, "-f", "avfoundation", "-list_devices", "true", "-i", "")
	// ffmpeg prints device list to stderr
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "0"
	}
	_ = cmd.Start()

	re := regexp.MustCompile(`\[(\d+)\]\s+Capture screen`)
	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		if m := re.FindStringSubmatch(scanner.Text()); m != nil {
			_ = cmd.Wait()
			return m[1]
		}
	}
	_ = cmd.Wait()
	return "0"
}

// buildArgs constructs the ffmpeg command-line arguments for the current platform.
func (f *FFmpegCapture) buildArgs() ([]string, error) {
	fps := strconv.Itoa(f.config.FPS)
	gop := strconv.Itoa(f.config.FPS * f.config.SegmentSec) // keyframe interval in frames
	segTime := strconv.Itoa(f.config.SegmentSec)
	scale := fmt.Sprintf("scale=%d:-2", f.config.MaxWidth)
	outputPattern := filepath.Join(f.segmentDir, "segment_%05d.ts")

	var inputArgs []string

	switch runtime.GOOS {
	case "darwin":
		// avfoundation doesn't support low framerates like 4fps.
		// Capture at 30fps and use fps filter to downsample before encoding.
		screenIdx := detectDarwinScreenDevice(f.config.FFmpegPath)
		log.Printf("Using avfoundation screen device index: %s", screenIdx)
		inputArgs = []string{
			"-f", "avfoundation",
			"-framerate", "30",
			"-capture_cursor", "1",
			"-i", screenIdx + ":none",
		}
		scale = fmt.Sprintf("fps=%s,%s", fps, scale)
	case "linux":
		if os.Getenv("WAYLAND_DISPLAY") != "" {
			inputArgs = []string{
				"-f", "pipewire",
				"-framerate", fps,
				"-i", "default",
			}
		} else {
			display := os.Getenv("DISPLAY")
			if display == "" {
				display = ":0"
			}
			inputArgs = []string{
				"-f", "x11grab",
				"-framerate", fps,
				"-i", display,
			}
		}
	default:
		return nil, fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	// Common args: overwrite, input, scale, encode, segment
	args := []string{"-y"}
	args = append(args, inputArgs...)
	args = append(args,
		"-vf", scale,
		"-c:v", "libx264",
		"-preset", "ultrafast",
		"-tune", "zerolatency",
		"-g", gop,
		"-keyint_min", gop,
		"-f", "segment",
		"-segment_time", segTime,
		"-segment_format", "mpegts",
		"-segment_list", f.segListPath,
		"-segment_list_type", "csv",
		"-reset_timestamps", "1",
		outputPattern,
	)

	return args, nil
}
