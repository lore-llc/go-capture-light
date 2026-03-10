# lore-watch-light

Lightweight screen capture agent for [Lore](https://getlore.ai). Streams screenshots to the Lore API whenever your screen changes. Zero runtime dependencies — just download the binary and run.

## How It Works

1. Captures screenshots as **JPEG** at the configured FPS rate (default 4 fps)
2. **Downscales** to `--resolution` (default `720p` / 1280px wide) using **ApproxBiLinear** interpolation, then re-encodes at **JPEG quality 65** to minimize bandwidth and server memory usage
3. Queues frames in a buffer and flushes them as **micro-batches** to the Lore API at the configured interval (default 3s)
4. On each flush, the buffer is **drained completely** — frames are sent and then discarded

## Quick Start

1. Download the latest binary for your platform from [Releases](../../releases).

2. Make it executable (macOS/Linux):
   ```bash
   chmod +x lore-watch-light-*
   ```

3. Run it:
   ```bash
   export LORE_API_KEY="your-api-key"
   ./lore-watch-light-linux-amd64
   ```

Press `Ctrl+C` to stop.

## CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--api-key` | `$LORE_API_KEY` | API key for authentication (required) |
| `--api-url` | `https://lore-agent-memory.onrender.com` | Lore API base URL |
| `--task` | `""` | Session task description |
| `--name` | `""` | Session name |
| `--user-id` | `$LORE_USER_ID` | User ID for multi-user tracking (optional — defaults to API key owner) |
| `--fps` | `4` | Capture frames per second |
| `--batch-interval` | `3s` | Interval between batch flushes |
| `--resolution` | `720p` | Screenshot resolution: `720p` (1280px) or `1080p` (1920px) |
| `--version` | | Print version and exit |

You can also set `LORE_API_KEY`, `LORE_API_URL`, and `LORE_USER_ID` in a `.env` or `.env.local` file in the working directory.

## Memory & Bandwidth Analysis

The `--resolution` flag controls the tradeoff between image quality and resource usage. Screenshots are captured at native resolution, then downscaled before encoding and sending.

### Per-frame size by resolution

| Display | Native | `--resolution 1080p` | `--resolution 720p` (default) |
|---------|--------|---------------------------|-------------------------------------|
| MacBook Pro 14" (3024x1964) | ~2-4 MB | ~500-800 KB | ~200-400 KB |
| MacBook Air 13" (2560x1664) | ~1.5-3 MB | ~400-700 KB | ~150-350 KB |
| 1920x1080 monitor | ~500-800 KB | no resize | ~200-400 KB |

### Client RAM usage (at default 4 fps, 3s batch interval = ~10-14 frames/batch)

| Component | Native (3K, no downscale) | `--resolution 720p` (default) |
|-----------|------------------------------|-------------------------------|
| Go runtime + binary | ~15 MB | ~15 MB |
| Pending frame buffer (15 frames) | ~30-60 MB | ~3-6 MB |
| Resize working memory (1 frame decode+scale+encode) | 0 MB | ~50 MB peak, released per frame |
| JSON + base64 encoding (per batch) | ~40-80 MB | ~4-8 MB |
| **Total peak** | **~85-155 MB** | **~72-79 MB** |

> Even with the decode/resize cost, the default 720p setting uses **less total RAM** than native because the pending buffer and JSON payload are ~10x smaller.

### Server RAM impact (per ingest request, Render 2 GB instance)

| | Native (3K, no downscale) | `--resolution 720p` (default) |
|---|------------------------------|-------------------------------|
| HTTP body received | ~40-80 MB | ~4-8 MB |
| Pydantic parse (copies strings) | ~40-80 MB | ~4-8 MB |
| Base64 decode (bytes) | ~30-60 MB | ~3-6 MB |
| **Peak per request** | **~110-220 MB** | **~11-22 MB** |
| Requests before 2 GB OOM (~400 MB base) | ~8 | ~70+ |

### Frame count per batch

At 4 fps with a 3s batch interval, the theoretical frame count is 12 per batch. In practice, the actual count **varies** (typically 10–14) because capture and resize run asynchronously:

- **Fewer than 12**: Screenshot capture (`screencapture`/`grim`) or resize took longer than the tick interval (250ms), so some frames weren't ready before the batch flush.
- **More than 12**: A previous slow frame finishes just after a batch flush, stacking into the next batch along with its normal frames.

This is expected behavior — no frames are lost, they just shift between adjacent batches.

### CPU usage

Each capture tick spawns a goroutine that runs: screenshot → JPEG decode → ApproxBiLinear downscale → JPEG re-encode (quality 65). At 4 fps with ~100-150ms per cycle, there is typically **1 concurrent goroutine** at any time.

| Component | CPU cost |
|-----------|----------|
| `screencapture` / `grim` (external process) | Low — OS handles it |
| JPEG decode (per frame) | Light — ~5-10ms |
| ApproxBiLinear scale (per frame) | Light — ~30-50ms |
| JPEG encode at quality 65 (per frame) | Light — ~5-15ms |
| **Total per core** | **~5-8% of one core** at 4 fps |

> **Default config (4 fps, 720p, ApproxBiLinear, quality 65)**: ~5-8% of one CPU core, ~72-79 MB peak RAM, ~1-3 MB/s upload. This is lightweight enough to run alongside normal desktop work on both macOS and Linux without noticeable impact.

### Scaling algorithm: ApproxBiLinear vs CatmullRom

We use `draw.ApproxBiLinear` from `golang.org/x/image/draw` for downscaling. This is a deliberate tradeoff:

| | ApproxBiLinear | CatmullRom |
|---|----------------|------------|
| Speed | ~30-50ms per frame | ~100-150ms per frame |
| CPU at 4 fps | ~5-8% of one core | ~15-20% of one core |
| Quality | Good — slight softness on small text | Excellent — sharp text/UI edges |
| Best for | Screen recordings, general monitoring | OCR, detailed UI analysis |

For screen recording and playback, ApproxBiLinear quality is more than sufficient. Text remains readable at 720p and the ~3x speed improvement significantly reduces CPU load, especially on single-core VMs or shared machines.

Both algorithms are pure Go (`golang.org/x/image/draw`) — no CGo, no platform-specific dependencies. Works identically on macOS and Linux.

### JPEG quality: 65 vs 80

Re-encoding at quality 65 instead of 80 reduces file size by ~30-40% with minimal visual difference for screenshots:

| Quality | Avg frame size (720p) | Visual impact |
|---------|----------------------|---------------|
| 80 | ~200-400 KB | Baseline — sharp |
| 65 | ~120-250 KB | Slight compression artifacts, text still fully readable |
| 50 | ~80-150 KB | Noticeable artifacts around text edges |

Quality 65 is the sweet spot: text remains clear for playback and review, while keeping bandwidth and server memory ~30% lower than quality 80.

### FPS optimization

The default FPS of 4 balances capture fidelity against resource usage:

| FPS | Tick interval | Frames/batch (3s) | CPU (one core) | Bandwidth |
|-----|--------------|-------------------|----------------|-----------|
| 2 | 500ms | ~6 | ~2-4% | ~0.5-1 MB/s |
| 3 | 333ms | ~9 | ~4-6% | ~0.7-1.5 MB/s |
| **4** (default) | **250ms** | **~12** | **~5-8%** | **~1-2 MB/s** |
| 5 | 200ms | ~15 | ~7-10% | ~1-3 MB/s |
| 10 | 100ms | ~30 | ~15-20% | ~2-5 MB/s |

- **4 fps** captures enough detail for smooth playback and UI state tracking without wasting resources.
- **2-3 fps** is recommended for resource-constrained environments (single-core VMs, CI runners).
- **5+ fps** is useful if you need to catch fast UI transitions but comes with proportionally higher CPU and bandwidth cost.

### Bandwidth per batch

| | Native 3K | 1080p | 720p (default) |
|---|-----------|-------|----------------|
| Raw JPEG | ~30-60 MB | ~8-12 MB | ~3-6 MB |
| Base64 JSON payload | ~40-80 MB | ~10-16 MB | ~4-8 MB |
| Upload rate (per 3s) | ~13-27 MB/s | ~3-5 MB/s | ~1-3 MB/s |

### Storage estimates (default settings: 4 fps, 720p, quality 65)

#### Frame data (individual JPEGs stored server-side)

| Duration | Frames captured | Estimated storage |
|----------|----------------|-------------------|
| 1 hour | 14,400 | ~1.7 – 3.5 GB |
| 24 hours | 345,600 | ~41 – 84 GB |

> Based on ~120–250 KB per frame at 720p / JPEG quality 65. Actual sizes depend on screen content — text-heavy screens compress better than media-rich ones.

#### Equivalent video storage (H.264 720p comparison)

If the same frames were encoded as H.264 video instead of individual JPEGs, inter-frame compression would dramatically reduce size:

| Duration | JPEG frames (current) | H.264 video equivalent |
|----------|-----------------------|------------------------|
| 1 hour | ~1.7 – 3.5 GB | ~100 – 300 MB |
| 24 hours | ~41 – 84 GB | ~2.4 – 7.2 GB |

> H.264 estimates assume ~0.2–0.7 Mbps for 720p screen content at 4 fps. Screen recordings compress extremely well due to low motion and large static regions between frames. The current architecture uses individual JPEGs for simplicity and random-access retrieval — each frame can be queried independently without decoding a video stream.

### Multi-User Deployment

When deploying across many VMs with a shared API key, pass `--user-id` to track each user's sessions separately:

```bash
./lore-watch-light --api-key "$SHARED_KEY" --user-id "user-42"
```

Query a user's sessions later via:

```bash
curl "https://lore-agent-memory.onrender.com/v1/sessions/list?user_id=user-42" \
  -H "X-API-Key: $SHARED_KEY"
```

You can also filter with `&limit=50` to control the number of results.

## Linux Screenshot Tools

On Linux, you need one of the following installed:

| Tool | Display Server |
|------|---------------|
| `grim` | Wayland |
| `import` (ImageMagick) | X11 |
| `scrot` | X11 |

On macOS, the built-in `screencapture` command is used automatically.

## Building from Source

Requires Go 1.21+.

```bash
# Build for current platform
make build

# Build for all platforms
make VERSION=0.1.0 build-all
```

Binaries are output to `dist/`.
