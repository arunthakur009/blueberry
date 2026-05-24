package manifest

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// Package is the parsed content of a .MANIFEST file inside a .bb archive,
// and also the unit stored in the repo index (BBINDEX).
type Package struct {
	Name          string    `toml:"name"`
	Version       string    `toml:"version"`
	Release       int       `toml:"release"`
	Arch          string    `toml:"arch"`
	Description   string    `toml:"description"`
	URL           string    `toml:"url"`
	License       string    `toml:"license"`
	Depends       []string  `toml:"depends"`
	Provides      []string  `toml:"provides"`
	Conflicts     []string  `toml:"conflicts"`
	Replaces      []string  `toml:"replaces"`
	Size          int64     `toml:"size"`
	InstalledSize int64     `toml:"installed_size"`
	BuildDate     time.Time `toml:"build_date"`
	Packager      string    `toml:"packager"`
	SHA256        string    `toml:"sha256"` // hash of the .bb file
	Filename      string    `toml:"filename"`
}

// FullVersion returns "version-release" (e.g. "1.2.4-1").
func (p *Package) FullVersion() string {
	return fmt.Sprintf("%s-%d", p.Version, p.Release)
}

// DepName strips any version constraint from a dependency string.
// e.g. "musl>=1.2.0" → "musl"
func DepName(dep string) string {
	for i, c := range dep {
		if c == '>' || c == '<' || c == '=' || c == '!' {
			return dep[:i]
		}
	}
	return dep
}

// Encode serialises a Package to the BBINDEX line format.
// Records are separated by a blank line.
func (p *Package) Encode(w io.Writer) {
	write := func(key, val string) {
		if val != "" {
			fmt.Fprintf(w, "%s: %s\n", key, val)
		}
	}
	write("name", p.Name)
	write("version", p.Version)
	fmt.Fprintf(w, "release: %d\n", p.Release)
	write("arch", p.Arch)
	write("description", p.Description)
	write("url", p.URL)
	write("license", p.License)
	if len(p.Depends) > 0 {
		fmt.Fprintf(w, "depends: %s\n", strings.Join(p.Depends, " "))
	}
	if len(p.Provides) > 0 {
		fmt.Fprintf(w, "provides: %s\n", strings.Join(p.Provides, " "))
	}
	if len(p.Conflicts) > 0 {
		fmt.Fprintf(w, "conflicts: %s\n", strings.Join(p.Conflicts, " "))
	}
	if len(p.Replaces) > 0 {
		fmt.Fprintf(w, "replaces: %s\n", strings.Join(p.Replaces, " "))
	}
	fmt.Fprintf(w, "size: %d\n", p.Size)
	fmt.Fprintf(w, "installed_size: %d\n", p.InstalledSize)
	if !p.BuildDate.IsZero() {
		fmt.Fprintf(w, "build_date: %s\n", p.BuildDate.UTC().Format(time.RFC3339))
	}
	write("packager", p.Packager)
	write("sha256", p.SHA256)
	write("filename", p.Filename)
	fmt.Fprintln(w)
}

// DecodeIndex reads a BBINDEX stream and returns all packages.
func DecodeIndex(r io.Reader) ([]*Package, error) {
	var pkgs []*Package
	cur := &Package{}
	inRecord := false

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if inRecord {
				pkgs = append(pkgs, cur)
				cur = &Package{}
				inRecord = false
			}
			continue
		}
		idx := strings.IndexByte(line, ':')
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		inRecord = true

		switch key {
		case "name":
			cur.Name = val
		case "version":
			cur.Version = val
		case "release":
			cur.Release, _ = strconv.Atoi(val)
		case "arch":
			cur.Arch = val
		case "description":
			cur.Description = val
		case "url":
			cur.URL = val
		case "license":
			cur.License = val
		case "depends":
			if val != "" {
				cur.Depends = strings.Fields(val)
			}
		case "provides":
			if val != "" {
				cur.Provides = strings.Fields(val)
			}
		case "conflicts":
			if val != "" {
				cur.Conflicts = strings.Fields(val)
			}
		case "replaces":
			if val != "" {
				cur.Replaces = strings.Fields(val)
			}
		case "size":
			cur.Size, _ = strconv.ParseInt(val, 10, 64)
		case "installed_size":
			cur.InstalledSize, _ = strconv.ParseInt(val, 10, 64)
		case "build_date":
			cur.BuildDate, _ = time.Parse(time.RFC3339, val)
		case "packager":
			cur.Packager = val
		case "sha256":
			cur.SHA256 = val
		case "filename":
			cur.Filename = val
		}
	}
	if inRecord {
		pkgs = append(pkgs, cur)
	}
	return pkgs, scanner.Err()
}

// EncodeTOML writes a .MANIFEST file (TOML-like, simple key=value).
func (p *Package) EncodeTOML(w io.Writer) {
	fmt.Fprintf(w, "name = %q\n", p.Name)
	fmt.Fprintf(w, "version = %q\n", p.Version)
	fmt.Fprintf(w, "release = %d\n", p.Release)
	fmt.Fprintf(w, "arch = %q\n", p.Arch)
	fmt.Fprintf(w, "description = %q\n", p.Description)
	fmt.Fprintf(w, "url = %q\n", p.URL)
	fmt.Fprintf(w, "license = %q\n", p.License)
	if len(p.Depends) > 0 {
		fmt.Fprintf(w, "depends = [%s]\n", joinQuoted(p.Depends))
	}
	if len(p.Provides) > 0 {
		fmt.Fprintf(w, "provides = [%s]\n", joinQuoted(p.Provides))
	}
	if len(p.Conflicts) > 0 {
		fmt.Fprintf(w, "conflicts = [%s]\n", joinQuoted(p.Conflicts))
	}
	if len(p.Replaces) > 0 {
		fmt.Fprintf(w, "replaces = [%s]\n", joinQuoted(p.Replaces))
	}
	fmt.Fprintf(w, "size = %d\n", p.Size)
	fmt.Fprintf(w, "installed_size = %d\n", p.InstalledSize)
	fmt.Fprintf(w, "build_date = %q\n", p.BuildDate.UTC().Format(time.RFC3339))
	fmt.Fprintf(w, "packager = %q\n", p.Packager)
	fmt.Fprintf(w, "sha256 = %q\n", p.SHA256)
}

// DecodeTOML reads a .MANIFEST file.
func DecodeTOML(r io.Reader) (*Package, error) {
	p := &Package{}
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])

		switch key {
		case "name":
			p.Name = unquote(val)
		case "version":
			p.Version = unquote(val)
		case "release":
			p.Release, _ = strconv.Atoi(val)
		case "arch":
			p.Arch = unquote(val)
		case "description":
			p.Description = unquote(val)
		case "url":
			p.URL = unquote(val)
		case "license":
			p.License = unquote(val)
		case "depends":
			p.Depends = parseStringArray(val)
		case "provides":
			p.Provides = parseStringArray(val)
		case "conflicts":
			p.Conflicts = parseStringArray(val)
		case "replaces":
			p.Replaces = parseStringArray(val)
		case "size":
			p.Size, _ = strconv.ParseInt(val, 10, 64)
		case "installed_size":
			p.InstalledSize, _ = strconv.ParseInt(val, 10, 64)
		case "build_date":
			p.BuildDate, _ = time.Parse(time.RFC3339, unquote(val))
		case "packager":
			p.Packager = unquote(val)
		case "sha256":
			p.SHA256 = unquote(val)
		}
	}
	if p.Name == "" {
		return nil, fmt.Errorf("manifest missing name field")
	}
	return p, scanner.Err()
}

func unquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	return s
}

func parseStringArray(s string) []string {
	s = strings.Trim(s, "[]")
	parts := strings.Split(s, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(unquote(strings.TrimSpace(p)))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func joinQuoted(ss []string) string {
	quoted := make([]string, len(ss))
	for i, s := range ss {
		quoted[i] = fmt.Sprintf("%q", s)
	}
	return strings.Join(quoted, ", ")
}
