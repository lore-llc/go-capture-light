# lore-watch-light

Lightweight screen capture agent for [Lore](https://getlore.ai). Streams screenshots to the Lore API whenever your screen changes. Zero dependencies — just download the binary and run.

## How It Works

1. Captures screenshots as **PNG** at the configured FPS rate (default 10 fps)
2. Runs **CRC32 motion detection** on the raw PNG bytes — PNG is deterministic, so identical screens produce identical hashes
3. Applies a **500ms cooldown** between accepted frames to avoid flooding during rapid changes
4. **Drops frames** that are identical to the previous one or within the cooldown window — when the screen is idle, no frames are sent
5. If the screen changed, **converts PNG → JPEG** (quality 80) to reduce payload size
6. Queues changed frames in a buffer and flushes them as **micro-batches** to the Lore API at the configured interval (default 3s)
7. On each flush, the buffer is **drained completely** — frames are sent and then discarded

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
| `--fps` | `10` | Capture frames per second |
| `--batch-interval` | `3s` | Interval between batch flushes |
| `--version` | | Print version and exit |

You can also set `LORE_API_KEY`, `LORE_API_URL`, and `LORE_USER_ID` in a `.env` or `.env.local` file in the working directory.

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
