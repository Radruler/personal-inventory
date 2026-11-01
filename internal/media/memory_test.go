package media

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// memBudgetKB is PLAN.md §2's peak budget for the whole container while a
// photo is processed: the Go process plus the vips child.
const memBudgetKB = 150 * 1024

// TestMemory48MPHEIC processes a 48 MP iPhone-shaped HEIC (8064x6048, a
// 16x12 grid of 512px HEVC tiles, irot 90°) through upload and the variant
// worker with every vips tool wrapped in GNU time, and checks that the
// largest vips child plus the Go process's own peak stay under 150 MB.
func TestMemory48MPHEIC(t *testing.T) {
	if testing.Short() {
		t.Skip("memory test skipped in -short mode")
	}
	if runtime.GOOS != "linux" {
		t.Skip("needs /proc")
	}
	if _, err := os.Stat("/usr/bin/time"); err != nil {
		t.Skip("needs GNU time at /usr/bin/time")
	}
	d, h := newEnv(t)

	src := filepath.Join(t.TempDir(), "IMG_4801.HEIC")
	var tiles []gridTile
	for i := 0; i < 3; i++ {
		tiles = append(tiles, encodeTile(t, d.Cfg, gradient(512, 512, uint8(60*i+20))))
	}
	writeGridFixture(t, src, 12, 16, 8064, 6048, tiles, irotBox(3))

	// Wrap vips, vipsthumbnail and vipsheader so every child's max RSS is logged.
	wrap := t.TempDir()
	logPath := filepath.Join(wrap, "rss.log")
	for _, tool := range []string{"vips", "vipsthumbnail", "vipsheader"} {
		real, err := exec.LookPath(tool)
		if err != nil {
			t.Skipf("%s not found", tool)
		}
		script := fmt.Sprintf("#!/bin/sh\nexec /usr/bin/time -a -o %q -f '%s %%M' %q \"$@\"\n", logPath, tool, real)
		if err := os.WriteFile(filepath.Join(wrap, tool), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	d.Cfg.VipsBin = wrap

	body, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	up := upload(t, h, 201, fileField("IMG_4801.HEIC", body))
	body = nil
	if up.Width == nil || *up.Width != 6048 || *up.Height != 8064 {
		t.Errorf("dims %v x %v, want 6048 x 8064", deref(up.Width), deref(up.Height))
	}
	runtime.GC()
	process(t, d)
	m := getMedia(t, h, up.ID)
	if m.VariantStatus != "ready" {
		t.Fatalf("variants: %s %v", m.VariantStatus, m.VariantError)
	}
	if img := decodeViaReal(t, DisplayPath(d.Cfg, up.SHA256)); img[0] != 1200 || img[1] != 1600 {
		t.Errorf("display %v, want 1200x1600", img)
	}

	maxChild, calls := maxRSS(t, logPath)
	goPeak := vmHWM(t)
	total := maxChild + goPeak
	t.Logf("%d vips calls; largest vips child %d MB, Go process peak %d MB, total %d MB (budget %d MB)",
		calls, maxChild/1024, goPeak/1024, total/1024, memBudgetKB/1024)
	if total >= memBudgetKB {
		t.Errorf("peak %d MB exceeds the %d MB budget", total/1024, memBudgetKB/1024)
	}

	// For comparison: decoding the same file whole, as plain vipsthumbnail does.
	os.Remove(logPath)
	if err := runVips(context.Background(), d.Cfg, "vipsthumbnail", OrigPath(d.Cfg, up.SHA256, "heic"),
		"--size", displayBox(), "-o", filepath.Join(t.TempDir(), "whole.webp")); err != nil {
		t.Fatal(err)
	}
	whole, _ := maxRSS(t, logPath)
	t.Logf("for comparison, vipsthumbnail decoding the grid whole peaks at %d MB", whole/1024)
}

// decodeViaReal returns width/height of an image using the real vipsheader.
func decodeViaReal(t *testing.T, path string) [2]int {
	t.Helper()
	var out [2]int
	for i, f := range []string{"width", "height"} {
		b, err := exec.Command("vipsheader", "-f", f, path).Output()
		if err != nil {
			t.Fatal(err)
		}
		out[i], _ = strconv.Atoi(strings.TrimSpace(string(b)))
	}
	return out
}

func maxRSS(t *testing.T, logPath string) (maxKB, calls int) {
	t.Helper()
	f, err := os.Open(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 2 {
			continue // "Command exited with non-zero status …"
		}
		kb, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		calls++
		maxKB = max(maxKB, kb)
	}
	if calls == 0 {
		t.Fatal("no vips calls were measured")
	}
	return maxKB, calls
}

func vmHWM(t *testing.T) int {
	t.Helper()
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, "VmHWM:"); ok {
			kb, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(v), " kB"))
			return kb
		}
	}
	t.Fatal("no VmHWM")
	return 0
}
