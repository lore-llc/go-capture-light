# lore-watch-light

Lightweight screen capture agent for [Lore](https://getlore.ai). Uses a persistent `ffmpeg` process to capture your screen as H.264 video segments and stream them to the Lore API.

## Install

```bash
curl -sSfL https://raw.githubusercontent.com/lore-llc/go-capture-light/main/install.sh | sh
```

This downloads the latest binary for your platform and installs `ffmpeg` (+ `xinput` on Linux for input tracking) if not already present.

## How It Works

1. Spawns a single persistent **ffmpeg** process that captures the screen at the configured FPS (default 4)
2. ffmpeg **downscales** to `--resolution` (default `720p` / 1280px wide) and encodes as **H.264** with `ultrafast` preset
3. Outputs self-contained **3-second MPEG-TS segments** (`.ts` files) to a temp directory
4. On Linux, spawns **xinput** to capture mouse and keyboard events (clicks, scrolls, moves, drags, keystrokes)
5. A segment watcher polls for new `.ts` files, drains buffered input actions, and **POSTs each segment + actions** to the Lore API
6. Segments are deleted locally after successful upload

## v0.2.0 Breaking Changes

v0.2.0 replaces the entire capture pipeline. **This is not backward-compatible with v0.1.x.**

| | v0.1.x (JPEG per-frame) | v0.2.0 (H.264 segments) |
|---|---|---|
| Capture method | `screencapture`/`grim`/`scrot` spawned 4x/sec | Single persistent `ffmpeg` process |
| Encoding | Individual JPEG frames (quality 65) | H.264 video (`libx264 ultrafast`) |
| Transport | Base64 JSON with `frames[]` | JSON with `frames: []` + `video_segment` (same endpoint) |
| Scaling | Go-side decode + ApproxBiLinear resize + re-encode | ffmpeg `-vf scale=1280:-2` |
| Dependencies | Go binary only + screenshot tool | Go binary + **ffmpeg** |

### Removed CLI flags

- `--batch-interval` — no longer applicable (segments are always 3 seconds)
- `--legacy` — no backward-compatible mode

### Removed dependencies

- `golang.org/x/image` — no longer needed (ffmpeg handles scaling)
- `screencapture` / `grim` / `scrot` / `import` — no longer needed

### New dependencies

- **ffmpeg** with `libx264` support (installed automatically by `install.sh`)
- **xinput** on Linux (installed automatically by `install.sh`) — for mouse + keyboard input tracking via X11

## Quick Start

1. Install (see above), or download from [Releases](../../releases).

2. Run:
   ```bash
   export LORE_API_KEY="your-api-key"
   lore-watch-light
   ```

3. Press `Ctrl+C` to stop. ffmpeg finalizes the current segment, the watcher sends it, then exits cleanly.

## CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--api-key` | `$LORE_API_KEY` | API key for authentication (required) |
| `--api-url` | `https://lore-agent-memory.onrender.com` | Lore API base URL |
| `--task` | `""` | Session task description |
| `--name` | `""` | Session name |
| `--user-id` | `$LORE_USER_ID` | User ID for multi-user tracking (optional) |
| `--fps` | `4` | Capture frames per second |
| `--resolution` | `720p` | Target resolution: `720p` (1280px) or `1080p` (1920px) |
| `--version` | | Print version and exit |

You can also set `LORE_API_KEY`, `LORE_API_URL`, and `LORE_USER_ID` in a `.env` or `.env.local` file in the working directory.

## Architecture

```
ffmpeg (1 persistent process)
    captures screen → scales to 720p → encodes H.264
    → outputs 3-second .ts segments to /tmp/lore_{session_id}/

xinput (1 persistent process, Linux only)
    captures mouse + keyboard events from X11
    → parsed by Go state machine → buffered as InputAction structs
    → adaptive throttle: 20ms/50ms/100ms for mouse moves based on buffer pressure

Go segment watcher (polls every 500ms)
    → reads new .ts file bytes
    → drains buffered input actions (clicks, keys, scrolls, moves, drags)
    → wraps in JSON with video_segment + actions[]
    → HTTP POST /v1/sessions/{session_id}/ingest/stream  (same endpoint as v0.1.x)
    → deletes local .ts file after successful send
```

### ffmpeg command (macOS)

```bash
ffmpeg -y -f avfoundation -framerate 4 -capture_cursor 1 -i "1:none" \
  -vf "scale=1280:-2" \
  -c:v libx264 -preset ultrafast -tune zerolatency \
  -g 12 -keyint_min 12 \
  -f segment -segment_time 3 -segment_format mpegts \
  -reset_timestamps 1 \
  /tmp/lore_{session_id}/segment_%05d.ts
```

### ffmpeg command (Linux X11)

```bash
ffmpeg -y -f x11grab -framerate 4 -i :0 \
  -vf "scale=1280:-2" \
  -c:v libx264 -preset ultrafast -tune zerolatency \
  -g 12 -keyint_min 12 \
  -f segment -segment_time 3 -segment_format mpegts \
  -reset_timestamps 1 \
  /tmp/lore_{session_id}/segment_%05d.ts
```

### ffmpeg command (Linux Wayland)

```bash
ffmpeg -y -f pipewire -framerate 4 -i default \
  -vf "scale=1280:-2" \
  ...same encoding flags...
```

### Key ffmpeg flags

| Flag | Purpose |
|---|---|
| `-framerate 4` | Capture at 4 fps |
| `-capture_cursor 1` | Include cursor in capture (macOS) |
| `-vf "scale=1280:-2"` | Downscale to 720p width, `-2` ensures even height (H.264 requirement) |
| `-preset ultrafast` | Minimize CPU usage |
| `-tune zerolatency` | No buffering, emit frames immediately |
| `-g 12 -keyint_min 12` | Force keyframe every 12 frames (= 3 seconds at 4fps) |
| `-f segment -segment_time 3` | Output self-contained 3-second segments |
| `-segment_format mpegts` | MPEG-TS container (concatenation-friendly) |
| `-reset_timestamps 1` | Each segment starts at t=0 |

## HTTP Transport

Uses the **same endpoint** as v0.1.x, with the same JSON structure but `frames: []` and a new `video_segment` field:

```
POST /v1/sessions/{session_id}/ingest/stream
Content-Type: application/json
X-API-Key: {api_key}
```

```json
{
  "batch_id": "segment-0",
  "frames": [],
  "actions": [
    {"type": "click", "timestamp": "2026-03-10T12:34:57.123Z", "metadata": {"x": 640, "y": 320}},
    {"type": "keypress", "timestamp": "2026-03-10T12:34:58.456Z", "metadata": {"key": "a"}},
    {"type": "move", "timestamp": "2026-03-10T12:34:58.789Z", "metadata": {"x": 700, "y": 350}}
  ],
  "app_context": [],
  "ax_snapshots": [],
  "clipboard": [],
  "window_geometry": [],
  "video_segment": {
    "format": "mpegts",
    "codec": "h264",
    "duration_sec": 3,
    "index": 0,
    "timestamp": "2026-03-10T12:34:56.789Z",
    "data_base64": "<base64-encoded .ts segment>"
  }
}
```

Typical payload size: **~100-350 KB** per 3-second segment (vs ~4-8 MB in v0.1.x).

See [SERVER-CHANGES-REQUIRED.md](SERVER-CHANGES-REQUIRED.md) for the server-side changes needed to handle the new `video_segment` field.

## Resource Usage

### v0.2.0 (H.264 segments) vs v0.1.x (JPEG per-frame)

| Metric | v0.1.x (JPEG) | v0.2.0 (H.264) | Improvement |
|---|---|---|---|
| Bandwidth | ~1-2 MB/s | ~0.05-0.15 MB/s | **~10-20x less** |
| HTTP payload per batch | ~4-8 MB (base64 JSON) | ~70-250 KB (binary .ts) | **~20-30x less** |
| Client RAM | ~72-79 MB | ~35-50 MB | **~40% less** |
| Client CPU | ~5-8% one core | ~3-5% one core | **~40% less** |
| Process spawns | 4/sec (screencapture) | 1 total (ffmpeg) | **eliminated** |
| Storage per hour | ~1.7-3.5 GB | ~150-500 MB | **~7-10x less** |

### Server RAM impact (per ingest request)

| | v0.1.x (JSON + base64) | v0.2.0 (binary .ts) |
|---|---|---|
| HTTP body | ~4-8 MB | ~70-250 KB |
| Processing overhead | ~11-22 MB | ~1-2 MB |
| **Peak per request** | **~11-22 MB** | **~1-2 MB** |
| Concurrent requests before 2GB OOM | ~70 | **~800+** |

### Segment size breakdown (3s at 4fps = 12 frames per segment)

| Component | Size |
|---|---|
| I-frame (keyframe, 1 per segment) | ~50-150 KB |
| P-frames (delta, 11 per segment) | ~2-10 KB each |
| **Total per segment** | **~70-250 KB** |

### FPS scaling

| FPS | Frames/segment | CPU (one core) | Bandwidth |
|-----|----------------|----------------|-----------|
| 2 | 6 | ~1-3% | ~0.02-0.08 MB/s |
| **4** (default) | **12** | **~3-5%** | **~0.05-0.15 MB/s** |
| 5 | 15 | ~4-6% | ~0.07-0.2 MB/s |
| 10 | 30 | ~8-12% | ~0.15-0.4 MB/s |

## Segment Stitching (Server-Side)

MPEG-TS segments are designed for concatenation. On the server:

```bash
# Lossless concatenation — no re-encoding
ffmpeg -f concat -safe 0 -i segments.txt -c copy session_full.mp4
```

Or on-the-fly as segments arrive:

```python
with open(f"sessions/{session_id}/full.ts", "ab") as f:
    f.write(segment_bytes)
```

## Graceful Shutdown

On `SIGINT` / `SIGTERM` (Ctrl+C, `kill`, systemd stop):

1. Input tracker is stopped (xinput killed, remaining actions buffered)
2. Go sends `SIGINT` to the ffmpeg subprocess
3. ffmpeg finalizes the current `.ts` segment and exits
4. Segment watcher picks up the final segment + drains remaining input actions
5. Final segment + actions are POSTed to the server
6. Process exits — **zero data loss on clean shutdown**

On hard crash (`SIGKILL`, power loss): at most ~3 seconds of footage is lost. MPEG-TS is resilient to truncation — partial segments are often still playable.

## Platform Support

| Platform | Screen capture | Input tracking | Status |
|----------|---------------|----------------|--------|
| macOS (Intel + ARM) | `avfoundation` via ffmpeg | Not supported (no-op stub) | Screen capture only |
| Linux X11 | `x11grab` via ffmpeg | `xinput` (mouse + keyboard) | Full support |
| Linux Wayland | `pipewire` via ffmpeg | Not yet implemented | Experimental — PipeWire support in ffmpeg is still maturing |

> **Note:** On macOS, the Go light client captures screen video via ffmpeg but does **not** track mouse/keyboard input. For full input tracking on macOS, use the [Swift client](../pre-alpha-swift-ios/) instead.

## Multi-User Deployment

```bash
./lore-watch-light --api-key "$SHARED_KEY" --user-id "user-42"
```

Query sessions:

```bash
curl "https://lore-agent-memory.onrender.com/v1/sessions/list?user_id=user-42" \
  -H "X-API-Key: $SHARED_KEY"
```

## Building from Source

Requires Go 1.21+ and ffmpeg.

```bash
# Build for current platform
make build

# Build for all platforms
make VERSION=0.2.0 build-all
```

Binaries are output to `dist/`.

## File Structure

```
pre-alpha-client-go-light/
  main.go              # Entry point, CLI flags, session + ffmpeg + input tracker + watcher lifecycle
  ffmpeg.go            # FFmpeg subprocess management, platform detection, command building
  input_linux.go       # Input tracking via xinput (mouse + keyboard) — Linux only
  input_darwin.go      # Input tracking stub (no-op) — macOS
  segment_watcher.go   # Polls temp dir for .ts segments, drains input actions, sends + cleans up
  client.go            # HTTP client (StartSession, SendSegment with actions)
  install.sh           # One-line installer (binary + ffmpeg + xinput)
  Makefile             # Build targets
```
