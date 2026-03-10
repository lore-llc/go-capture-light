package main

import (
	"bytes"
	"image"
	"image/jpeg"
	"log"

	"golang.org/x/image/draw"
)

// Preset max widths for downscaling.
const (
	MaxWidth720p    = 1280
	MaxWidth1080p   = 1920
	DefaultMaxWidth = MaxWidth720p
)

// Log the first resize only (avoid spamming every frame).
var resizeLogged bool

// MaybeResize decodes JPEG bytes, downscales if width > maxWidth
// (maintaining aspect ratio), and re-encodes at quality 65.
// Uses ApproxBiLinear for fast scaling with acceptable quality.
// Returns original bytes unchanged if no resize is needed.
func MaybeResize(jpegBytes []byte, maxWidth int) []byte {
	if maxWidth <= 0 {
		return jpegBytes
	}

	// Decode JPEG
	src, err := jpeg.Decode(bytes.NewReader(jpegBytes))
	if err != nil {
		log.Printf("resize: decode failed, sending original: %v", err)
		return jpegBytes
	}

	bounds := src.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()

	if srcW <= maxWidth {
		return jpegBytes
	}

	// Scale proportionally
	dstW := maxWidth
	dstH := srcH * maxWidth / srcW

	if !resizeLogged {
		log.Printf("Resizing: %dx%d → %dx%d (max_width=%d)", srcW, srcH, dstW, dstH, maxWidth)
		resizeLogged = true
	}

	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, bounds, draw.Over, nil)

	// Re-encode as JPEG
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 65}); err != nil {
		log.Printf("resize: encode failed, sending original: %v", err)
		return jpegBytes
	}

	return buf.Bytes()
}
