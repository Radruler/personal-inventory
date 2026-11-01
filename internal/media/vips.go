package media

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/radruler/personal-inventory/internal/config"
)

// Memory (PLAN.md §2: peak < 150 MB for the container while processing a
// photo, vips child included):
//
//   - vips children run with VIPS_CONCURRENCY=1 and one at a time (the
//     worker has concurrency 1).
//   - JPEG and WebP use shrink-on-load inside vipsthumbnail; PNG streams.
//   - HEIC has no shrink-on-load and libheif decodes a grid in one piece, so
//     grid HEICs larger than bandPixelBudget are cut into bands of whole
//     tiles (see heif.go), each decoded and box-shrunk by its own vips
//     process and stitched into a small PPM, which is then resized, colour
//     converted and rotated. A 48 MP iPhone photo peaks around 60 MB this way
//     instead of ~290 MB.
//   - The display variant is generated first and the thumbnail is made from
//     it, so the original is decoded once.

// bandPixelBudget is the largest band (in pixels) decoded at once: about
// 25 MB of RGB plus 12 MB of YCbCr inside libheif. Tests lower it.
var bandPixelBudget = 8 << 20

// vipsCommand builds a vips tool invocation with the low-memory environment.
func vipsCommand(ctx context.Context, cfg config.Config, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, cfg.Bin(name), args...)
	cmd.Env = append(os.Environ(), "VIPS_CONCURRENCY=1")
	return cmd
}

func runVips(ctx context.Context, cfg config.Config, name string, args ...string) error {
	out, err := vipsCommand(ctx, cfg, name, args...).CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%s: %v: %s", name, err, firstLines(out, 3))
	}
	return nil
}

func firstLines(b []byte, n int) string {
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	var keep []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		keep = append(keep, l)
		if len(keep) == n {
			break
		}
	}
	return strings.Join(keep, " | ")
}

// tempSibling returns a unique temp name next to dst that keeps dst's
// extension (vips picks the output format from it).
func tempSibling(dst string) string {
	var r [6]byte
	_, _ = rand.Read(r[:])
	ext := filepath.Ext(dst)
	return strings.TrimSuffix(dst, ext) + "." + hex.EncodeToString(r[:]) + ".tmp" + ext
}

// GenerateVariants writes the display (1600px WebP q82, /media/var) and
// thumb (400px WebP q75, /data/thumbs) variants for the original at src:
// auto-rotated, converted to sRGB, metadata stripped. The display variant is
// made first and the thumbnail from it. Each file is written to a temp name
// in its destination directory and renamed into place.
// Frozen signature (used by seed); internals belong to thread B.
func GenerateVariants(ctx context.Context, cfg config.Config, src, sha string) error {
	src, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	display, err := filepath.Abs(DisplayPath(cfg, sha))
	if err != nil {
		return err
	}
	thumb, err := filepath.Abs(ThumbPath(cfg, sha))
	if err != nil {
		return err
	}
	for _, p := range []string{display, thumb} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
	}
	tmp := tempSibling(display)
	if err := makeDisplay(ctx, cfg, src, tmp); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, display); err != nil {
		os.Remove(tmp)
		return err
	}
	tmp = tempSibling(thumb)
	err = runVips(ctx, cfg, "vipsthumbnail", display,
		"--size", fmt.Sprintf("%dx%d>", ThumbSize, ThumbSize),
		"-o", fmt.Sprintf("%s[Q=%d,strip]", tmp, ThumbQuality))
	if err == nil {
		err = os.Rename(tmp, thumb)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}

func displaySave(dst string) string { return fmt.Sprintf("%s[Q=%d,strip]", dst, DisplayQuality) }

func displayBox() string { return fmt.Sprintf("%dx%d>", DisplaySize, DisplaySize) } // fit, never upscale

// makeDisplay writes the display variant of src to dst (a .webp path).
func makeDisplay(ctx context.Context, cfg config.Config, src, dst string) error {
	f, g, closeFn := openGrid(src)
	defer closeFn()
	if g != nil && g.width*g.height > bandPixelBudget {
		return displayBanded(ctx, cfg, f, g, dst)
	}
	return runVips(ctx, cfg, "vipsthumbnail", src, "--size", displayBox(),
		"--export-profile", "srgb", "-o", displaySave(dst))
}

// openGrid returns the parsed file and grid when src is a HEIC grid image
// that can be split. Any parse problem means "let vips handle it whole".
// closeFn is never nil.
func openGrid(src string) (f *heifFile, g *gridInfo, closeFn func()) {
	closeFn = func() {}
	fh, err := os.Open(src)
	if err != nil {
		return nil, nil, closeFn
	}
	closeFn = func() { fh.Close() }
	var head [12]byte
	if _, err := io.ReadFull(fh, head[:]); err != nil || string(head[4:8]) != "ftyp" {
		return nil, nil, closeFn
	}
	st, err := fh.Stat()
	if err != nil {
		return nil, nil, closeFn
	}
	if f, err = parseHEIF(fh, st.Size()); err != nil {
		return nil, nil, closeFn
	}
	g, ok, err := f.grid()
	if err != nil || !ok {
		return f, nil, closeFn
	}
	return f, g, closeFn
}

// shrinkFactor is the largest power of two that divides the tile size and
// still leaves the long edge at least DisplaySize. Box-shrinking whole tiles
// by it is exact, so bands stitch without seams.
func shrinkFactor(g *gridInfo) int {
	long := max(g.width, g.height)
	f := 1
	for long/(f*2) >= DisplaySize && g.tileW%(f*2) == 0 && g.tileH%(f*2) == 0 {
		f *= 2
	}
	return f
}

func displayBanded(ctx context.Context, cfg config.Config, f *heifFile, g *gridInfo, dst string) error {
	work, err := os.MkdirTemp(filepath.Dir(dst), ".work-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)

	factor := shrinkFactor(g)
	rowsPerBand := max(1, bandPixelBudget/(g.cols*g.tileW*g.tileH))
	outW, outH := g.width/factor, g.height/factor

	joinedPath := filepath.Join(work, "joined.ppm")
	jf, err := os.Create(joinedPath)
	if err != nil {
		return err
	}
	defer jf.Close()
	jw := bufio.NewWriterSize(jf, 256<<10)
	var first *ppmHeader
	written := 0
	bandPath := filepath.Join(work, "band.heic")
	for r0 := 0; r0 < g.rows; r0 += rowsPerBand {
		r1 := min(g.rows, r0+rowsPerBand)
		spec := gridSpec{rows: r1 - r0, cols: g.cols, width: g.cols * g.tileW, height: (r1 - r0) * g.tileH, props: g.gridProps}
		for _, t := range g.tiles[r0*g.cols : r1*g.cols] {
			data, err := f.itemData(t)
			if err != nil {
				return err
			}
			spec.tiles = append(spec.tiles, gridTile{props: f.tileProps(t), data: data})
		}
		bf, err := os.Create(bandPath)
		if err != nil {
			return err
		}
		werr := writeGridHEIF(bf, spec)
		if cerr := bf.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			return werr
		}
		spec.tiles = nil

		args := []string{"copy", bandPath, ".ppm"}
		if factor > 1 {
			fs := strconv.Itoa(factor)
			args = []string{"shrink", bandPath, ".ppm", fs, fs}
		}
		cmd := vipsCommand(ctx, cfg, "vips", args...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.StdoutPipe()
		if err != nil {
			return err
		}
		if err := cmd.Start(); err != nil {
			return err
		}
		h, copyErr := copyBand(out, jw, first, outW, outH, &written)
		_, _ = io.Copy(io.Discard, out)
		waitErr := cmd.Wait()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if waitErr != nil {
			return fmt.Errorf("vips %s (band %d): %v: %s", args[0], r0, waitErr, firstLines(stderr.Bytes(), 3))
		}
		if copyErr != nil {
			return fmt.Errorf("band %d: %w", r0, copyErr)
		}
		if first == nil {
			first = h
		}
	}
	if written != outH {
		return fmt.Errorf("bands produced %d rows, want %d", written, outH)
	}
	if err := jw.Flush(); err != nil {
		return err
	}
	if err := jf.Close(); err != nil {
		return err
	}

	args := []string{joinedPath, "--size", displayBox()}
	if len(g.icc) > 0 {
		icc := filepath.Join(work, "in.icc")
		if err := os.WriteFile(icc, g.icc, 0o600); err != nil {
			return err
		}
		args = append(args, "--import-profile", icc, "--export-profile", "srgb")
	}
	if len(g.transforms) == 0 {
		return runVips(ctx, cfg, "vipsthumbnail", append(args, "-o", displaySave(dst))...)
	}
	cur := filepath.Join(work, "resized.v")
	if err := runVips(ctx, cfg, "vipsthumbnail", append(args, "-o", cur)...); err != nil {
		return err
	}
	for i, t := range g.transforms {
		op := strings.Fields(t) // e.g. "rot d90"
		next := filepath.Join(work, fmt.Sprintf("t%d.v", i))
		if i == len(g.transforms)-1 {
			next = displaySave(dst)
		}
		if err := runVips(ctx, cfg, "vips", op[0], cur, next, op[1]); err != nil {
			return err
		}
		cur = next
	}
	return nil
}

type ppmHeader struct {
	magic         string
	width, height int
	maxval        int
}

func (h ppmHeader) rowBytes(width int) int {
	ch := 3
	if h.magic == "P5" {
		ch = 1
	}
	bps := 1
	if h.maxval > 255 {
		bps = 2
	}
	return width * ch * bps
}

// copyBand reads one PPM band from r and appends its first outW columns to
// w, stopping at outH rows in total. The joined header is written before the
// first band's rows.
func copyBand(r io.Reader, w *bufio.Writer, first *ppmHeader, outW, outH int, written *int) (*ppmHeader, error) {
	br := bufio.NewReaderSize(r, 256<<10)
	h, err := readPPMHeader(br)
	if err != nil {
		return nil, err
	}
	if h.width < outW {
		return nil, fmt.Errorf("band is %d wide, want at least %d", h.width, outW)
	}
	if first == nil {
		if _, err := fmt.Fprintf(w, "%s\n%d %d\n%d\n", h.magic, outW, outH, h.maxval); err != nil {
			return nil, err
		}
	} else if first.magic != h.magic || first.maxval != h.maxval {
		return nil, errors.New("bands differ in format")
	}
	row := make([]byte, h.rowBytes(h.width))
	keep := h.rowBytes(outW)
	for y := 0; y < h.height; y++ {
		if _, err := io.ReadFull(br, row); err != nil {
			return nil, err
		}
		if *written < outH {
			if _, err := w.Write(row[:keep]); err != nil {
				return nil, err
			}
			*written++
		}
	}
	return h, nil
}

func readPPMHeader(br *bufio.Reader) (*ppmHeader, error) {
	var toks []string
	var cur []byte
	for len(toks) < 4 {
		c, err := br.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("ppm header: %w", err)
		}
		switch {
		case c == '#' && len(cur) == 0:
			if _, err := br.ReadString('\n'); err != nil {
				return nil, fmt.Errorf("ppm header: %w", err)
			}
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			if len(cur) > 0 {
				toks = append(toks, string(cur))
				cur = cur[:0]
			}
		default:
			cur = append(cur, c)
			if len(cur) > 16 {
				return nil, errors.New("ppm header: bad token")
			}
		}
	}
	h := &ppmHeader{magic: toks[0]}
	if h.magic != "P6" && h.magic != "P5" {
		return nil, fmt.Errorf("ppm header: unexpected %q", h.magic)
	}
	var err1, err2, err3 error
	h.width, err1 = strconv.Atoi(toks[1])
	h.height, err2 = strconv.Atoi(toks[2])
	h.maxval, err3 = strconv.Atoi(toks[3])
	if err := errors.Join(err1, err2, err3); err != nil || h.width < 1 || h.height < 1 || h.maxval < 1 || h.maxval > 65535 {
		return nil, errors.New("ppm header: bad numbers")
	}
	return h, nil
}

// Dimensions returns width and height as reported by vipsheader (before any
// EXIF rotation; HEIF transformations are already applied by libheif).
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
