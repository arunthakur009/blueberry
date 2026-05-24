// Package db manages the installed-package database at /var/lib/bpm/db/.
//
// Layout:
//
//	/var/lib/bpm/db/installed/<name>/MANIFEST   package metadata
//	/var/lib/bpm/db/installed/<name>/FILES       newline-separated installed paths
//	/var/lib/bpm/db/world                        explicitly-installed package names
package db

import (
	"blueberry.linux/bpm/internal/manifest"
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	DefaultRoot    = "/"
	DefaultDBPath  = "/var/lib/bpm/db"
	DefaultWorld   = "/var/lib/bpm/db/world"
	DefaultCachePkg = "/var/lib/bpm/cache/packages"
	DefaultCacheIdx = "/var/lib/bpm/cache/indices"
)

// DB is the local installed-package database.
type DB struct {
	root      string // filesystem root (usually "/")
	dbPath    string // absolute path to db directory
	worldPath string
}

// New opens the database at dbPath inside root.
func New(root, dbPath string) (*DB, error) {
	abs := filepath.Join(root, dbPath)
	if err := os.MkdirAll(abs+"/installed", 0755); err != nil {
		return nil, err
	}
	return &DB{root: root, dbPath: abs, worldPath: filepath.Join(abs, "world")}, nil
}

// Default returns a DB using the standard system paths.
func Default() (*DB, error) {
	return New(DefaultRoot, DefaultDBPath)
}

// IsInstalled reports whether pkg is installed.
func (d *DB) IsInstalled(name string) bool {
	_, err := os.Stat(filepath.Join(d.dbPath, "installed", name, "MANIFEST"))
	return err == nil
}

// Get returns the manifest for an installed package.
func (d *DB) Get(name string) (*manifest.Package, error) {
	f, err := os.Open(filepath.Join(d.dbPath, "installed", name, "MANIFEST"))
	if err != nil {
		return nil, fmt.Errorf("package %q not installed", name)
	}
	defer f.Close()
	return manifest.DecodeTOML(f)
}

// Files returns the list of files owned by an installed package.
func (d *DB) Files(name string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(d.dbPath, "installed", name, "FILES"))
	if err != nil {
		return nil, err
	}
	var files []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

// List returns all installed package names.
func (d *DB) List() ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(d.dbPath, "installed"))
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

// Record writes a new package entry after successful installation.
func (d *DB) Record(pkg *manifest.Package, installedFiles []string) error {
	dir := filepath.Join(d.dbPath, "installed", pkg.Name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Write MANIFEST
	mf, err := os.Create(filepath.Join(dir, "MANIFEST"))
	if err != nil {
		return err
	}
	pkg.EncodeTOML(mf)
	mf.Close()

	// Write FILES
	ff, err := os.Create(filepath.Join(dir, "FILES"))
	if err != nil {
		return err
	}
	for _, f := range installedFiles {
		fmt.Fprintln(ff, f)
	}
	ff.Close()

	return nil
}

// Remove deletes the database entry for a package.
func (d *DB) Remove(name string) error {
	return os.RemoveAll(filepath.Join(d.dbPath, "installed", name))
}

// World returns the set of explicitly-installed package names.
func (d *DB) World() (map[string]bool, error) {
	f, err := os.Open(d.worldPath)
	if os.IsNotExist(err) {
		return make(map[string]bool), nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	world := make(map[string]bool)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		name := strings.TrimSpace(sc.Text())
		if name != "" {
			world[name] = true
		}
	}
	return world, sc.Err()
}

// AddToWorld adds a package to the world set.
func (d *DB) AddToWorld(name string) error {
	world, err := d.World()
	if err != nil {
		return err
	}
	world[name] = true
	return d.writeWorld(world)
}

// RemoveFromWorld removes a package from the world set.
func (d *DB) RemoveFromWorld(name string) error {
	world, err := d.World()
	if err != nil {
		return err
	}
	delete(world, name)
	return d.writeWorld(world)
}

func (d *DB) writeWorld(world map[string]bool) error {
	f, err := os.Create(d.worldPath)
	if err != nil {
		return err
	}
	defer f.Close()
	for name := range world {
		fmt.Fprintln(f, name)
	}
	return nil
}

// CheckOwner returns the package name that owns the given path, or "".
func (d *DB) CheckOwner(path string) (string, error) {
	installed, err := d.List()
	if err != nil {
		return "", err
	}
	for _, name := range installed {
		files, err := d.Files(name)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f == path {
				return name, nil
			}
		}
	}
	return "", nil
}
