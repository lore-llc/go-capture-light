#!/bin/sh
set -e

REPO="lore-llc/go-capture-light"
BIN_NAME="lore-watch-light"

# Detect OS and architecture
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

# Map generic architecture names to Go architecture names
case "$ARCH" in
    x86_64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

# Validate OS
if [ "$OS" != "linux" ] && [ "$OS" != "darwin" ]; then
    echo "Unsupported operating system: $OS"
    exit 1
fi

# Construct the download URL based on expected release asset names
# e.g., go-capture-light-linux-amd64
ASSET_NAME="${BIN_NAME}-${OS}-${ARCH}"
DOWNLOAD_URL="https://github.com/${REPO}/releases/latest/download/${ASSET_NAME}"

echo "Downloading ${ASSET_NAME} from latest release..."
# Download the binary (-f will fail silently on server errors like 404)
if ! curl -sSL -f "$DOWNLOAD_URL" -o "$BIN_NAME"; then
    echo "Error: Failed to download $ASSET_NAME."
    echo "Make sure a GitHub Release exists with this exact asset name."
    exit 1
fi

chmod +x "$BIN_NAME"

echo "Installing to /usr/local/bin (may prompt for sudo password)..."
sudo mv "$BIN_NAME" /usr/local/bin/

# Ensure ffmpeg is installed (required for H.264 screen capture)
if ! command -v ffmpeg >/dev/null 2>&1; then
    echo "ffmpeg not found. Installing..."
    case "$OS" in
        darwin)
            if command -v brew >/dev/null 2>&1; then
                brew install ffmpeg
            else
                echo "Error: ffmpeg is required. Install Homebrew (https://brew.sh) then run: brew install ffmpeg"
                exit 1
            fi
            ;;
        linux)
            if command -v apt-get >/dev/null 2>&1; then
                sudo apt-get update && sudo apt-get install -y ffmpeg
            elif command -v dnf >/dev/null 2>&1; then
                sudo dnf install -y ffmpeg
            elif command -v pacman >/dev/null 2>&1; then
                sudo pacman -S --noconfirm ffmpeg
            else
                echo "Error: ffmpeg is required. Install it manually for your distro."
                exit 1
            fi
            ;;
    esac
    echo "ffmpeg installed successfully."
else
    echo "ffmpeg found: $(ffmpeg -version 2>&1 | head -1)"
fi

# Ensure xinput is installed on Linux (required for input tracking)
if [ "$OS" = "linux" ]; then
    if ! command -v xinput >/dev/null 2>&1; then
        echo "xinput not found. Installing..."
        if command -v apt-get >/dev/null 2>&1; then
            sudo apt-get update && sudo apt-get install -y xinput
        elif command -v dnf >/dev/null 2>&1; then
            sudo dnf install -y xinput
        elif command -v pacman >/dev/null 2>&1; then
            sudo pacman -S --noconfirm xorg-xinput
        else
            echo "Warning: xinput is required for input tracking. Install it manually."
        fi
        echo "xinput installed successfully."
    else
        echo "xinput found."
    fi
fi

echo "Success! You can now run '$BIN_NAME'."
