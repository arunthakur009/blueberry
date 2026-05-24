// Package build parses and executes BBUILD recipes.
//
// A BBUILD is a shell script with structured variable declarations at the top
// and optional build() and package() functions. The builder executes it in a
// clean chroot-like environment and produces a .bb archive.
package build

import (
	"blueberry.linux/bpm/internal/archive"
	"blueberry.linux/bpm/internal/manifest"
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Recipe is the parsed header of a BBUILD file.
type Recipe struct {
	Name        string
	Version     string
	Release     int
	Description string
	URL         string
	License     string
	Arch        []string
	Depends     []string
	MakeDepends []string
	Source      []string
	Checksums   []string
	Packager    string
	Path        string // path to the BBUILD file
}

// Parse reads a BBUILD file and extracts header variables.
// It does NOT execute the script; variables must be plain assignments.
func Parse(path string) (*Recipe, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := &Recipe{
		Path:    path,
		Release: 1,
		Arch:    []string{"x86_64", "aarch64"},
	}
	sc := bufio.NewScanner(f)
	var pendingKey, pendingVal string
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)

		// Accumulate multi-line array values
		if pendingKey != "" {
			pendingVal += " " + trimmed
			if strings.Contains(trimmed, ")") {
				line = pendingKey + "=" + pendingVal
				trimmed = line
				pendingKey, pendingVal = "", ""
			} else {
				continue
			}
		}

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasSuffix(trimmed, "() {") || strings.HasSuffix(trimmed, "(){") {
			break
		}
		idx := strings.IndexByte(trimmed, '=')
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(trimmed[:idx])
		val := strings.TrimSpace(trimmed[idx+1:])

		// Detect unclosed array — accumulate until closing )
		if strings.HasPrefix(val, "(") && !strings.Contains(val, ")") {
			pendingKey = key
			pendingVal = val
			continue
		}

		switch key {
		case "name":
			r.Name = unquote(val)
		case "version":
			r.Version = unquote(val)
		case "release":
			fmt.Sscanf(unquote(val), "%d", &r.Release)
		case "description":
			r.Description = unquote(val)
		case "url":
			r.URL = unquote(val)
		case "license":
			r.License = unquote(val)
		case "arch":
			r.Arch = parseArray(val)
		case "depends":
			r.Depends = parseArray(val)
		case "makedepends":
			r.MakeDepends = parseArray(val)
		case "source":
			r.Source = parseArray(val)
		case "checksums":
			r.Checksums = parseArray(val)
		case "packager":
			r.Packager = unquote(val)
		}
	}
	if r.Name == "" || r.Version == "" {
		return nil, fmt.Errorf("%s: missing required fields (name, version)", path)
	}
	return r, sc.Err()
}

// BuildOptions configures a package build.
type BuildOptions struct {
	WorkDir string // temporary build workspace (created if empty)
	Arch    string // target arch
	Jobs    int    // parallel make jobs (0 = nproc)
	Output  string // directory to write the .bb file into
	Topdir  string // repo root, exposed as $TOPDIR in BBUILD scripts
}

// Build executes a BBUILD recipe and writes the resulting .bb archive.
// Returns the path to the created .bb file.
func Build(recipe *Recipe, opts BuildOptions) (string, error) {
	if opts.Jobs == 0 {
		opts.Jobs = runtime.NumCPU()
	}
	if opts.Arch == "" {
		opts.Arch = runtime.GOARCH
	}
	if opts.Output == "" {
		opts.Output = "."
	}

	// Create work directory
	workDir := opts.WorkDir
	if workDir == "" {
		var err error
		workDir, err = os.MkdirTemp("", "bpm-build-"+recipe.Name+"-")
		if err != nil {
			return "", err
		}
		defer os.RemoveAll(workDir)
	}

	srcDir := filepath.Join(workDir, "src")
	pkgDir := filepath.Join(workDir, "pkg")
	for _, d := range []string{srcDir, pkgDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return "", err
		}
	}

	// Download sources (expand $name/$version/$release in URLs)
	recipeDir := filepath.Dir(recipe.Path)
	for i, src := range recipe.Source {
		src = expandRecipeVars(src, recipe)
		// Resolve relative paths against the BBUILD directory
		if !strings.HasPrefix(src, "http://") && !strings.HasPrefix(src, "https://") && !filepath.IsAbs(src) {
			src = filepath.Join(recipeDir, src)
		}
		if err := fetchSource(src, srcDir, safeGet(recipe.Checksums, i)); err != nil {
			return "", fmt.Errorf("fetch %s: %w", src, err)
		}
	}

	// Execute build() and package() via bash — BBUILD files use bash syntax (arrays etc.)
	script := buildScript(recipe, srcDir, pkgDir, opts.Jobs)
	cmd := exec.Command("bash", "-e", "-")
	cmd.Stdin = strings.NewReader(script)
	cmd.Dir = srcDir
	cmd.Env = buildEnv(recipe, srcDir, pkgDir, opts)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("build failed: %w", err)
	}

	// Collect files from pkgDir
	files := make(map[string]string)
	err := filepath.Walk(pkgDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || path == pkgDir {
			return err
		}
		rel, _ := filepath.Rel(pkgDir, path)
		files[rel] = path
		return nil
	})
	if err != nil {
		return "", err
	}

	// Build manifest
	meta := &manifest.Package{
		Name:        recipe.Name,
		Version:     recipe.Version,
		Release:     recipe.Release,
		Arch:        opts.Arch,
		Description: recipe.Description,
		URL:         recipe.URL,
		License:     recipe.License,
		Depends:     recipe.Depends,
		Packager:    recipe.Packager,
		BuildDate:   time.Now().UTC(),
	}

	// Write .bb archive
	outName := fmt.Sprintf("%s-%s-%d-%s.bb", recipe.Name, recipe.Version, recipe.Release, opts.Arch)
	outPath := filepath.Join(opts.Output, outName)
	out, err := os.Create(outPath)
	if err != nil {
		return "", err
	}
	defer out.Close()

	var buf bytes.Buffer
	if err := archive.Create(io.MultiWriter(out, &buf), meta, files, nil); err != nil {
		os.Remove(outPath)
		return "", err
	}

	fmt.Printf("Created %s\n", outPath)
	return outPath, nil
}

func buildScript(r *Recipe, srcDir, pkgDir string, jobs int) string {
	bbuildContent, _ := os.ReadFile(r.Path)
	return fmt.Sprintf(`
srcdir=%q
pkgdir=%q
MAKEFLAGS="-j%d"
name=%q
version=%q
release=%d

%s

build
package
`, srcDir, pkgDir, jobs, r.Name, r.Version, r.Release, string(bbuildContent))
}

func buildEnv(r *Recipe, srcDir, pkgDir string, opts BuildOptions) []string {
	home := os.Getenv("HOME")
	if home == "" {
		home = "/root"
	}
	hostPath := os.Getenv("PATH")
	if hostPath == "" {
		hostPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	}
	return []string{
		"HOME=" + home,
		"PATH=" + hostPath,
		"LANG=C",
		"LC_ALL=C",
		"srcdir=" + srcDir,
		"pkgdir=" + pkgDir,
		"name=" + r.Name,
		"version=" + r.Version,
		fmt.Sprintf("release=%d", r.Release),
		fmt.Sprintf("MAKEFLAGS=-j%d", opts.Jobs),
		"CFLAGS=-Os -pipe",
		"CXXFLAGS=-Os -pipe",
		"ARCH=" + opts.Arch,
		"TOPDIR=" + opts.Topdir,
	}
}

func fetchSource(src, destDir, checksum string) error {
	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
		filename := filepath.Base(src)
		dest := filepath.Join(destDir, filename)

		cmd := exec.Command("wget", "-q", "-O", dest, src)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return err
		}
		if err := verifyChecksum(dest, checksum); err != nil {
			return err
		}
		return extractArchive(dest, destDir)
	}
	// local file — copy into srcdir
	dest := filepath.Join(destDir, filepath.Base(src))
	return copyFile(src, dest)
}

func extractArchive(path, destDir string) error {
	name := strings.ToLower(filepath.Base(path))
	var cmd *exec.Cmd
	switch {
	case strings.HasSuffix(name, ".tar.gz") || strings.HasSuffix(name, ".tgz"):
		cmd = exec.Command("tar", "-xzf", path, "-C", destDir)
	case strings.HasSuffix(name, ".tar.bz2") || strings.HasSuffix(name, ".tbz2"):
		cmd = exec.Command("tar", "-xjf", path, "-C", destDir)
	case strings.HasSuffix(name, ".tar.xz") || strings.HasSuffix(name, ".txz"):
		cmd = exec.Command("tar", "-xJf", path, "-C", destDir)
	case strings.HasSuffix(name, ".tar.zst"):
		cmd = exec.Command("tar", "--zstd", "-xf", path, "-C", destDir)
	case strings.HasSuffix(name, ".zip"):
		cmd = exec.Command("unzip", "-q", path, "-d", destDir)
	default:
		return nil // not an archive, leave as-is
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func verifyChecksum(path, checksum string) error {
	if checksum == "" || checksum == "SKIP" {
		return nil
	}
	hash, err := archive.SHA256File(path)
	if err != nil {
		return err
	}
	expected := strings.TrimPrefix(checksum, "sha256:")
	if hash != expected {
		return fmt.Errorf("checksum mismatch for %s: got %s want %s", path, hash, expected)
	}
	return nil
}

func expandRecipeVars(s string, r *Recipe) string {
	s = strings.ReplaceAll(s, "$name", r.Name)
	s = strings.ReplaceAll(s, "${name}", r.Name)
	s = strings.ReplaceAll(s, "$version", r.Version)
	s = strings.ReplaceAll(s, "${version}", r.Version)
	s = strings.ReplaceAll(s, "$release", fmt.Sprintf("%d", r.Release))
	s = strings.ReplaceAll(s, "${release}", fmt.Sprintf("%d", r.Release))
	return s
}

func safeGet(ss []string, i int) string {
	if i < len(ss) {
		return ss[i]
	}
	return ""
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && ((s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'')) {
		return s[1 : len(s)-1]
	}
	return s
}

func parseArray(s string) []string {
	s = strings.TrimSpace(s)
	// Handle ("a" "b" "c") syntax
	s = strings.Trim(s, "()")
	var parts []string
	for _, part := range strings.Fields(s) {
		part = strings.Trim(part, `"'`)
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}
