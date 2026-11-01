package media

import (
	"path/filepath"

	"github.com/radruler/personal-inventory/internal/config"
)

// Storage layout (PLAN.md §2, §5). Originals and display variants live on
// the HDD pool (/media); thumbnails live on the SSD (/data) so grid browsing
// never spins up the disks.
//
//	/media/orig/ab/cd/<sha256>.<ext>   original, byte for byte
//	/media/var/ab/<sha256>.webp        display: 1600px WebP q82
//	/data/thumbs/ab/<sha256>.webp      thumb: 400px WebP q75

// OrigPath is where the original bytes for sha are stored.
func OrigPath(cfg config.Config, sha, ext string) string {
	return filepath.Join(cfg.MediaDir, "orig", sha[0:2], sha[2:4], sha+"."+ext)
}

// DisplayPath is where the display variant for sha is stored.
func DisplayPath(cfg config.Config, sha string) string {
	return filepath.Join(cfg.MediaDir, "var", sha[0:2], sha+".webp")
}

// ThumbPath is where the thumbnail for sha is stored.
func ThumbPath(cfg config.Config, sha string) string {
	return filepath.Join(cfg.DataDir, "thumbs", sha[0:2], sha+".webp")
}

// Variant specs. Changing these requires `inv media rebuild`.
const (
	ThumbSize      = 400
	ThumbQuality   = 75
	DisplaySize    = 1600
	DisplayQuality = 82
)

// URL returns the root-relative URL for a variant: thumb | display | orig.
func URL(sha, variant string) string { return "/media/" + sha + "/" + variant }
