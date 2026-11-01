package media

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/radruler/personal-inventory/internal/config"
)

// GenerateVariants writes the thumb and display WebP variants for the
// original at src (auto-rotated, metadata stripped). Each file is written to
// a temp name in its destination directory and renamed into place.
// Frozen signature (used by seed); internals belong to thread B.
func GenerateVariants(ctx context.Context, cfg config.Config, src, sha string) error {
	for _, v := range []struct {
		dst     string
		size, q int
	}{
		{ThumbPath(cfg, sha), ThumbSize, ThumbQuality},
		{DisplayPath(cfg, sha), DisplaySize, DisplayQuality},
	} {
		if err := os.MkdirAll(filepath.Dir(v.dst), 0o755); err != nil {
			return err
		}
		tmp := strings.TrimSuffix(v.dst, ".webp") + ".tmp.webp"
		size := fmt.Sprintf("%dx%d>", v.size, v.size) // fit inside the box, never upscale
		cmd := exec.CommandContext(ctx, cfg.Bin("vipsthumbnail"), src,
			"--size", size, "-o", tmp+fmt.Sprintf("[Q=%d,strip]", v.q))
		if out, err := cmd.CombinedOutput(); err != nil {
			os.Remove(tmp)
			return fmt.Errorf("vipsthumbnail: %v: %s", err, strings.TrimSpace(string(out)))
		}
		if err := os.Rename(tmp, v.dst); err != nil {
			os.Remove(tmp)
			return err
		}
	}
	return nil
}

// Dimensions returns width and height as reported by vipsheader (before any
// EXIF rotation).
func Dimensions(ctx context.Context, cfg config.Config, src string) (int, int, error) {
	get := func(field string) (int, error) {
		out, err := exec.CommandContext(ctx, cfg.Bin("vipsheader"), "-f", field, src).Output()
		if err != nil {
			return 0, fmt.Errorf("vipsheader %s: %w", field, err)
		}
		return strconv.Atoi(strings.TrimSpace(string(out)))
	}
	w, err := get("width")
	if err != nil {
		return 0, 0, err
	}
	h, err := get("height")
	return w, h, err
}
