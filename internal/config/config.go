// Package config reads runtime configuration from the environment.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config is the process configuration. Every field has an INV_* variable.
type Config struct {
	Addr           string     // INV_ADDR, default ":8480"
	DataDir        string     // INV_DATA_DIR, default "/data" (SSD: DB, thumbnails)
	MediaDir       string     // INV_MEDIA_DIR, default "/media" (HDD: originals, display variants)
	DBPath         string     // INV_DB_PATH, default "$INV_DATA_DIR/inv.db"
	Token          string     // INV_TOKEN; when set, /api writes need "Authorization: Bearer <token>"
	MaxUploadBytes int64      // INV_MAX_UPLOAD_MB, default 64
	LogLevel       slog.Level // INV_LOG_LEVEL: debug|info|warn|error, default info
	AccessLog      bool       // INV_ACCESS_LOG=1 logs every request (off by default)
	VipsBin        string     // INV_VIPS_BIN_DIR: directory holding vips/vipsthumbnail/vipsheader; default $PATH
}

// FromEnv builds a Config from INV_* environment variables.
func FromEnv() (Config, error) {
	c := Config{
		Addr:     env("INV_ADDR", ":8480"),
		DataDir:  env("INV_DATA_DIR", "/data"),
		MediaDir: env("INV_MEDIA_DIR", "/media"),
		Token:    os.Getenv("INV_TOKEN"),
		VipsBin:  os.Getenv("INV_VIPS_BIN_DIR"),
	}
	c.DBPath = env("INV_DB_PATH", filepath.Join(c.DataDir, "inv.db"))

	mb, err := strconv.ParseInt(env("INV_MAX_UPLOAD_MB", "64"), 10, 64)
	if err != nil || mb <= 0 {
		return c, fmt.Errorf("INV_MAX_UPLOAD_MB: want a positive integer")
	}
	c.MaxUploadBytes = mb << 20

	if err := c.LogLevel.UnmarshalText([]byte(env("INV_LOG_LEVEL", "info"))); err != nil {
		return c, fmt.Errorf("INV_LOG_LEVEL: %w", err)
	}
	c.AccessLog = truthy(os.Getenv("INV_ACCESS_LOG"))
	return c, nil
}

// ForTest returns a Config rooted in dir (DataDir=dir/data, MediaDir=dir/media).
func ForTest(dir string) Config {
	c := Config{
		Addr:           "127.0.0.1:0",
		DataDir:        filepath.Join(dir, "data"),
		MediaDir:       filepath.Join(dir, "media"),
		MaxUploadBytes: 64 << 20,
		LogLevel:       slog.LevelWarn,
	}
	c.DBPath = filepath.Join(c.DataDir, "inv.db")
	return c
}

// Bin returns the path of a vips tool (vips, vipsthumbnail, vipsheader).
func (c Config) Bin(name string) string {
	if c.VipsBin == "" {
		return name
	}
	return filepath.Join(c.VipsBin, name)
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func truthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
