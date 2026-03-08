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

echo "Success! You can now run '$BIN_NAME'."
