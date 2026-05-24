// Package repo manages repository configuration and package indices.
//
// Repos are configured in /etc/bpm/repos.d/*.conf (simple key=value).
// Indices are fetched as BBINDEX.zst files and cached locally.
package repo

import (
	"blueberry.linux/bpm/internal/manifest"
	"bytes"
	"crypto/sha256"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

func sha256New() hash.Hash { return sha256.New() }

const (
	DefaultReposDir = "/etc/bpm/repos.d"
	DefaultCacheDir = "/var/lib/bpm/cache/indices"
	IndexFile       = "BBINDEX.zst"
)

// Repo holds the configuration for a single package repository.
type Repo struct {
	Name    string
	URL     string
	Enabled bool
}

// Index is a parsed package index from a repository.
type Index struct {
	Repo     *Repo
	Packages map[string]*manifest.Package // name → package
}

// Manager coordinates all configured repositories.
type Manager struct {
	reposDir string
	cacheDir string
	client   *http.Client
}

// NewManager creates a Manager using the given config and cache directories.
func NewManager(reposDir, cacheDir string) *Manager {
	return &Manager{
		reposDir: reposDir,
		cacheDir: cacheDir,
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

// DefaultManager returns a Manager with system paths.
func DefaultManager() *Manager {
	return NewManager(DefaultReposDir, DefaultCacheDir)
}

// LoadRepos reads all *.conf files from the repos directory.
func (m *Manager) LoadRepos() ([]*Repo, error) {
	entries, err := os.ReadDir(m.reposDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var repos []*Repo
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".conf") {
			continue
		}
		r, err := parseRepoConf(filepath.Join(m.reposDir, e.Name()))
		if err != nil {
			fmt.Fprintf(os.Stderr, "warn: %s: %v\n", e.Name(), err)
			continue
		}
		if r.Enabled {
			repos = append(repos, r)
		}
	}
	return repos, nil
}

func parseRepoConf(path string) (*Repo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	r := &Repo{Enabled: true}
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
		case "name":
			r.Name = val
		case "url":
			r.URL = val
		case "enabled":
			r.Enabled = val == "true" || val == "1" || val == "yes"
		}
	}
	if r.Name == "" || r.URL == "" {
		return nil, fmt.Errorf("%s: missing name or url", path)
	}
	return r, nil
}

// Update fetches and caches the BBINDEX for all repos.
func (m *Manager) Update() error {
	repos, err := m.LoadRepos()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(m.cacheDir, 0755); err != nil {
		return err
	}
	for _, r := range repos {
		fmt.Printf("Updating %s... ", r.Name)
		if err := m.fetchIndex(r); err != nil {
			fmt.Printf("error: %v\n", err)
		} else {
			fmt.Println("ok")
		}
	}
	return nil
}

func (m *Manager) fetchIndex(r *Repo) error {
	url := strings.TrimRight(r.URL, "/") + "/" + IndexFile
	resp, err := m.client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(m.cacheDir, r.Name+".zst"), data, 0644)
}

// LoadIndex reads the cached index for a repo.
func (m *Manager) LoadIndex(r *Repo) (*Index, error) {
	path := filepath.Join(m.cacheDir, r.Name+".zst")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("no cached index for %s — run 'bpm update'", r.Name)
	}
	dec, err := zstd.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer dec.Close()

	pkgs, err := manifest.DecodeIndex(dec)
	if err != nil {
		return nil, err
	}

	idx := &Index{
		Repo:     r,
		Packages: make(map[string]*manifest.Package, len(pkgs)),
	}
	for _, p := range pkgs {
		idx.Packages[p.Name] = p
		// also index by provides
		for _, prov := range p.Provides {
			name := manifest.DepName(prov)
			if _, exists := idx.Packages[name]; !exists {
				idx.Packages[name] = p
			}
		}
	}
	return idx, nil
}

// AllIndices loads all cached indices.
func (m *Manager) AllIndices() ([]*Index, error) {
	repos, err := m.LoadRepos()
	if err != nil {
		return nil, err
	}
	var indices []*Index
	for _, r := range repos {
		idx, err := m.LoadIndex(r)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warn: %v\n", err)
			continue
		}
		indices = append(indices, idx)
	}
	return indices, nil
}

// Find searches all indices for a package name. First match wins (repo order).
func (m *Manager) Find(name string) (*manifest.Package, *Repo, error) {
	indices, err := m.AllIndices()
	if err != nil {
		return nil, nil, err
	}
	for _, idx := range indices {
		if p, ok := idx.Packages[name]; ok {
			return p, idx.Repo, nil
		}
	}
	return nil, nil, fmt.Errorf("package %q not found in any repository", name)
}

// Search returns all packages matching query (case-insensitive substring in name or description).
func (m *Manager) Search(query string) ([]*manifest.Package, error) {
	indices, err := m.AllIndices()
	if err != nil {
		return nil, err
	}
	query = strings.ToLower(query)
	seen := make(map[string]bool)
	var results []*manifest.Package
	for _, idx := range indices {
		for _, p := range idx.Packages {
			if seen[p.Name] {
				continue
			}
			if strings.Contains(strings.ToLower(p.Name), query) ||
				strings.Contains(strings.ToLower(p.Description), query) {
				results = append(results, p)
				seen[p.Name] = true
			}
		}
	}
	return results, nil
}

// Download fetches a .bb file for pkg into cacheDir. Returns the local path.
func (m *Manager) Download(pkg *manifest.Package, r *Repo) (string, error) {
	cacheDir := "/var/lib/bpm/cache/packages"
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return "", err
	}
	dest := filepath.Join(cacheDir, pkg.Filename)

	// Use cached copy if present and sha256 matches
	if existing, err := os.Stat(dest); err == nil && existing.Size() > 0 {
		if hash, err := hashFile(dest); err == nil && hash == pkg.SHA256 {
			return dest, nil
		}
	}

	url := strings.TrimRight(r.URL, "/") + "/" + pkg.Filename
	fmt.Printf("  Downloading %s... ", pkg.Filename)
	resp, err := m.client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("HTTP %d fetching %s", resp.StatusCode, url)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	fmt.Printf("%.1f KB\n", float64(len(data))/1024)

	if err := os.WriteFile(dest, data, 0644); err != nil {
		return "", err
	}

	// Verify
	if pkg.SHA256 != "" {
		hash, err := hashFile(dest)
		if err != nil {
			return "", err
		}
		if hash != pkg.SHA256 {
			os.Remove(dest)
			return "", fmt.Errorf("sha256 mismatch for %s", pkg.Filename)
		}
	}
	return dest, nil
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
