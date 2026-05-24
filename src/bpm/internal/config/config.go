// Package config loads bpm's runtime configuration from /etc/bpm/bpm.conf.
package config

import (
	"os"
	"strings"
)

const DefaultConfigPath = "/etc/bpm/bpm.conf"

// Config holds runtime options for bpm.
type Config struct {
	Root     string // install root, default "/"
	CacheDir string // package cache directory
	ReposDir string // repos.d directory
	DBPath   string // installed-packages database path
	Arch     string // target architecture (e.g. x86_64)
}

// Default returns a Config populated from defaults and the config file.
func Default() *Config {
	c := &Config{
		Root:     "/",
		CacheDir: "/var/lib/bpm/cache",
		ReposDir: "/etc/bpm/repos.d",
		DBPath:   "/var/lib/bpm/db",
		Arch:     detectArch(),
	}
	_ = c.loadFile(DefaultConfigPath)
	return c
}

func (c *Config) loadFile(path string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.Trim(strings.TrimSpace(line[idx+1:]), `"`)
		switch key {
		case "root":
			c.Root = val
		case "cache_dir":
			c.CacheDir = val
		case "repos_dir":
			c.ReposDir = val
		case "db_path":
			c.DBPath = val
		case "arch":
			c.Arch = val
		}
	}
	return nil
}

func detectArch() string {
	// Read from uname -m equivalent via /proc/version or hardcode based on build
	data, err := os.ReadFile("/proc/sys/kernel/arch")
	if err == nil {
		return strings.TrimSpace(string(data))
	}
	// Fallback: compiled-in GOARCH mapping
	return goarchToDistroArch()
}

func goarchToDistroArch() string {
	// This is resolved at compile time via the build tag mechanism
	// Default to x86_64 for the common case
	return "x86_64"
}
