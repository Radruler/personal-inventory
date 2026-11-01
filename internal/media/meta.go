package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/radruler/personal-inventory/internal/config"
)

// fileType is a supported upload type, detected from magic bytes.
type fileType struct {
	mime string
	ext  string
}

func (t fileType) isImage() bool { return strings.HasPrefix(t.mime, "image/") }

// sniffLen is how many leading bytes sniff looks at.
const sniffLen = 512

// HEIF brands (ISO/IEC 23008-12 Annex B and the HEVC brands Apple writes).
var (
	heicBrands = map[string]bool{"heic": true, "heix": true, "heim": true, "heis": true, "hevc": true, "hevx": true, "hevm": true, "hevs": true}
	heifBrands = map[string]bool{"mif1": true, "msf1": true, "mif2": true}
)

// sniff detects the type from the first bytes of a file. ok is false for
// anything the API does not store (PLAN.md §5: JPEG, PNG, WebP, HEIC/HEIF,
// and PDF/HTML for reference archives).
func sniff(b []byte) (fileType, bool) {
	switch {
	case bytes.HasPrefix(b, []byte{0xff, 0xd8, 0xff}):
		return fileType{"image/jpeg", "jpg"}, true
	case bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")):
		return fileType{"image/png", "png"}, true
	case len(b) >= 12 && string(b[0:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return fileType{"image/webp", "webp"}, true
	case len(b) >= 12 && string(b[4:8]) == "ftyp":
		return sniffFtyp(b)
	case bytes.HasPrefix(b, []byte("%PDF-")):
		return fileType{"application/pdf", "pdf"}, true
	}
	if strings.HasPrefix(http.DetectContentType(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))), "text/html") {
		return fileType{"text/html", "html"}, true
	}
	return fileType{}, false
}

// sniffFtyp classifies an ISOBMFF ftyp box: HEVC-coded HEIF is accepted;
// AVIF and video (mp4, mov) are not.
func sniffFtyp(b []byte) (fileType, bool) {
	size := int(binary.BigEndian.Uint32(b[0:4]))
	if size < 16 || size > len(b) {
		size = len(b) - len(b)%4
	}
	major := string(b[8:12])
	brands := []string{major}
	for i := 16; i+4 <= size; i += 4 {
		brands = append(brands, string(b[i:i+4]))
	}
	heic, heif := false, false
	for _, br := range brands {
		switch {
		case br == "avif" || br == "avis":
			if br == major {
				return fileType{}, false
			}
		case heicBrands[br]:
			heic = true
		case heifBrands[br]:
			heif = true
		}
	}
	switch {
	case heic:
		return fileType{"image/heic", "heic"}, true
	case heif && major != "mp41" && major != "mp42" && major != "isom" && major != "qt  ":
		return fileType{"image/heif", "heif"}, true
	}
	return fileType{}, false
}

// imageMeta is what we record about an image at upload time.
type imageMeta struct {
	width, height *int
	takenAt       *string
	exif          map[string]any // nil when the file carries no EXIF we keep
}

// exifKeep is the EXIF subset stored in media.exif, by libexif tag name.
// Only IFD0 (ifd0) and the Exif sub-IFD (ifd2) are read; GPS (ifd3) never is.
var exifKeep = map[string]bool{
	"Make": true, "Model": true, "Software": true, "HostComputer": true, "Orientation": true,
	"DateTimeOriginal": true, "SubSecTimeOriginal": true, "OffsetTimeOriginal": true,
	"DateTimeDigitized": true, "OffsetTime": true,
	"LensMake": true, "LensModel": true,
	"ExposureTime": true, "FNumber": true, "ISOSpeedRatings": true, "PhotographicSensitivity": true,
	"FocalLength": true, "FocalLengthIn35mmFilm": true, "ExposureBiasValue": true,
	"Flash": true, "WhiteBalance": true, "ColorSpace": true,
}

// vipsheader -a prints EXIF entries as
//
//	exif-ifd2-FNumber: 89/50 (1.78, Rational, 1 components, 8 bytes)
//
// that is: raw value, then libexif's formatted value, format, counts.
var exifEntry = regexp.MustCompile(`, (ASCII|Byte|SByte|Short|SShort|Long|SLong|Rational|SRational|Undefined|Float|Double), \d+ components?, \d+ bytes?\)$`)

// readImageMeta runs vipsheader -a (header only; no pixel decode) and
// extracts post-rotation dimensions, taken_at and the EXIF subset.
func readImageMeta(ctx context.Context, cfg config.Config, path string) (imageMeta, error) {
	var m imageMeta
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := vipsCommand(ctx, cfg, "vipsheader", "-a", path).Output()
	if err != nil {
		return m, fmt.Errorf("vipsheader: %w", err)
	}
	return parseVipsHeader(string(out)), nil
}

func parseVipsHeader(out string) imageMeta {
	var m imageMeta
	var w, h, orientation int
	exif := map[string]any{}
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(line, ": ")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch {
		case k == "width":
			w, _ = strconv.Atoi(v)
		case k == "height":
			h, _ = strconv.Atoi(v)
		case k == "orientation":
			// vips only reports this when it still has to rotate (JPEG,
			// PNG, WebP); libheif has already applied HEIF irot/imir.
			orientation, _ = strconv.Atoi(v)
		case strings.HasPrefix(k, "exif-ifd0-") || strings.HasPrefix(k, "exif-ifd2-"):
			name := k[len("exif-ifd0-"):]
			if !exifKeep[name] {
				continue
			}
			if val, ok := exifValue(name, v); ok {
				exif[name] = val
			}
		}
	}
	if w > 0 && h > 0 {
		if orientation >= 5 && orientation <= 8 {
			w, h = h, w
		}
		m.width, m.height = &w, &h
	}
	if len(exif) > 0 {
		m.exif = exif
	}
	if s, ok := exif["DateTimeOriginal"].(string); ok {
		sub, _ := exif["SubSecTimeOriginal"].(string)
		off, _ := exif["OffsetTimeOriginal"].(string)
		m.takenAt = takenAt(s, sub, off)
	}
	return m
}

// exifValue converts one vipsheader EXIF value to JSON: ASCII as a string,
// integers as numbers, rationals as numbers (ExposureTime keeps "1/120").
func exifValue(name, v string) (any, bool) {
	loc := exifEntry.FindStringSubmatchIndex(v)
	if loc == nil {
		return nil, false
	}
	format := v[loc[2]:loc[3]]
	head := v[:loc[0]] // "RAW (FORMATTED"
	raw := head
	if format == "ASCII" {
		// raw and formatted are the same string: "S (S"
		if n := len(head); n%2 == 0 && n >= 2 {
			l := (n - 2) / 2
			if head[l:l+2] == " (" && head[:l] == head[l+2:] {
				raw = head[:l]
			} else if i := strings.LastIndex(head, " ("); i >= 0 {
				raw = head[:i]
			}
		} else if i := strings.LastIndex(head, " ("); i >= 0 {
			raw = head[:i]
		}
		raw = strings.TrimSpace(strings.TrimRight(raw, "\x00"))
		if raw == "" {
			return nil, false
		}
		return raw, true
	}
	if i := strings.Index(head, " ("); i >= 0 {
		raw = head[:i]
	}
	raw = strings.TrimSpace(raw)
	switch format {
	case "Byte", "SByte", "Short", "SShort", "Long", "SLong":
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
			return n, true
		}
		return nil, false
	case "Rational", "SRational":
		num, den, ok := strings.Cut(raw, "/")
		if !ok {
			return nil, false
		}
		a, err1 := strconv.ParseFloat(num, 64)
		b, err2 := strconv.ParseFloat(den, 64)
		if err1 != nil || err2 != nil || b == 0 {
			return nil, false
		}
		if name == "ExposureTime" {
			return raw, true
		}
		return math.Round(a/b*10000) / 10000, true
	case "Float", "Double":
		if f, err := strconv.ParseFloat(raw, 64); err == nil {
			return f, true
		}
	}
	return nil, false
}

var offsetRe = regexp.MustCompile(`^([+-])(\d{2}):(\d{2})$`)

// takenAt converts EXIF DateTimeOriginal ("2026:10:30 14:05:09") plus
// optional sub-seconds and offset ("-07:00") to the stored form: UTC with Z
// when the offset is known, else local time without a zone.
func takenAt(dt, subsec, offset string) *string {
	t, err := time.Parse("2006:01:02 15:04:05", strings.TrimSpace(dt))
	if err != nil || t.Year() < 1900 {
		return nil
	}
	if subsec = strings.TrimSpace(subsec); subsec != "" {
		if len(subsec) > 3 {
			subsec = subsec[:3]
		}
		for len(subsec) < 3 {
			subsec += "0"
		}
		if ms, err := strconv.Atoi(subsec); err == nil {
			t = t.Add(time.Duration(ms) * time.Millisecond)
		}
	}
	if m := offsetRe.FindStringSubmatch(strings.TrimSpace(offset)); m != nil {
		hh, _ := strconv.Atoi(m[2])
		mm, _ := strconv.Atoi(m[3])
		secs := hh*3600 + mm*60
		if m[1] == "-" {
			secs = -secs
		}
		s := t.Add(-time.Duration(secs) * time.Second).UTC().Format("2006-01-02T15:04:05.000Z")
		return &s
	}
	s := t.Format("2006-01-02T15:04:05.000")
	return &s
}

func exifJSON(m map[string]any) any {
	if m == nil {
		return nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil
	}
	return string(b)
}
