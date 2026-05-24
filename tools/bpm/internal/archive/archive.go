// Package archive handles reading and writing .bb package archives.
// A .bb file is a zstd-compressed tar containing:
//
//	.MANIFEST    - package metadata (TOML-like)
//	.CHECKSUMS   - sha256:<hash>  path/to/file lines
//	.SCRIPTS/    - optional lifecycle scripts
//	<files>      - installed files at their final paths (relative to /)
package archive

import (
	"archive/tar"
	"blueberry.linux/bpm/internal/manifest"
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"
)

const (
	ScriptPreInstall  = ".SCRIPTS/pre-install"
	ScriptPostInstall = ".SCRIPTS/post-install"
	ScriptPreRemove   = ".SCRIPTS/pre-remove"
	ScriptPostRemove  = ".SCRIPTS/post-remove"
)

// Package is a read-only view of an opened .bb archive.
type Package struct {
	Manifest  *manifest.Package
	Checksums map[string]string // path → sha256 hex
	scripts   map[string][]byte
	files     []*tar.Header
	data      []byte // raw zstd bytes for streaming extraction
}

// Open reads a .bb archive from r and returns its metadata.
// file content is not extracted until Extract is called.
func Open(r io.Reader) (*Package, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	dec, err := zstd.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("open zstd: %w", err)
	}
	defer dec.Close()

	tr := tar.NewReader(dec)
	pkg := &Package{
		Checksums: make(map[string]string),
		scripts:   make(map[string][]byte),
		data:      data,
	}

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar: %w", err)
		}

		body, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("read entry %s: %w", hdr.Name, err)
		}

		switch hdr.Name {
		case ".MANIFEST":
			pkg.Manifest, err = manifest.DecodeTOML(bytes.NewReader(body))
			if err != nil {
				return nil, fmt.Errorf("parse manifest: %w", err)
			}
		case ".CHECKSUMS":
			for _, line := range strings.Split(string(body), "\n") {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				parts := strings.SplitN(line, "  ", 2)
				if len(parts) != 2 {
					continue
				}
				hash := strings.TrimPrefix(parts[0], "sha256:")
				pkg.Checksums[parts[1]] = hash
			}
		case ScriptPreInstall, ScriptPostInstall, ScriptPreRemove, ScriptPostRemove:
			pkg.scripts[hdr.Name] = body
		default:
			if !strings.HasPrefix(hdr.Name, ".") {
				pkg.files = append(pkg.files, hdr)
			}
		}
	}

	if pkg.Manifest == nil {
		return nil, fmt.Errorf("archive missing .MANIFEST")
	}
	return pkg, nil
}

// Extract installs all files from the archive into destDir.
// Returns the list of installed paths (relative to destDir).
func (p *Package) Extract(destDir string) ([]string, error) {
	dec, err := zstd.NewReader(bytes.NewReader(p.data))
	if err != nil {
		return nil, err
	}
	defer dec.Close()

	tr := tar.NewReader(dec)
	var installed []string

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if strings.HasPrefix(hdr.Name, ".") {
			io.Copy(io.Discard, tr)
			continue
		}

		target := filepath.Join(destDir, hdr.Name)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, hdr.FileInfo().Mode()); err != nil {
				return nil, err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return nil, err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, hdr.FileInfo().Mode())
			if err != nil {
				return nil, err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return nil, err
			}
			f.Close()
			installed = append(installed, hdr.Name)
		case tar.TypeSymlink:
			os.Remove(target)
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return nil, err
			}
			installed = append(installed, hdr.Name)
		case tar.TypeLink:
			linkTarget := filepath.Join(destDir, hdr.Linkname)
			os.Remove(target)
			if err := os.Link(linkTarget, target); err != nil {
				return nil, err
			}
			installed = append(installed, hdr.Name)
		}
	}
	return installed, nil
}

// Script returns the body of a lifecycle script, or nil if absent.
func (p *Package) Script(name string) []byte {
	return p.scripts[name]
}

// Create builds a .bb archive.
// files maps archive paths (relative, no leading /) to source paths on disk.
// scripts maps script names (e.g. ScriptPostInstall) to their content.
func Create(w io.Writer, meta *manifest.Package, files map[string]string, scripts map[string][]byte) error {
	enc, err := zstd.NewWriter(w, zstd.WithEncoderLevel(zstd.SpeedBestCompression))
	if err != nil {
		return err
	}

	tw := tar.NewWriter(enc)

	// Build checksums in parallel with writing files
	checksums := &bytes.Buffer{}

	// Write real files first and collect checksums
	for archivePath, srcPath := range files {
		info, err := os.Lstat(srcPath)
		if err != nil {
			return fmt.Errorf("%s: %w", srcPath, err)
		}

		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = archivePath

		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(srcPath)
			if err != nil {
				return err
			}
			hdr.Linkname = link
		}

		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}

		if info.Mode().IsRegular() {
			f, err := os.Open(srcPath)
			if err != nil {
				return err
			}
			h := sha256.New()
			if _, err := io.Copy(io.MultiWriter(tw, h), f); err != nil {
				f.Close()
				return err
			}
			f.Close()
			fmt.Fprintf(checksums, "sha256:%x  %s\n", h.Sum(nil), archivePath)
			meta.InstalledSize += info.Size()
		}
	}

	// Write .CHECKSUMS
	if err := writeEntry(tw, ".CHECKSUMS", checksums.Bytes()); err != nil {
		return err
	}

	// Write scripts
	for name, body := range scripts {
		if err := writeEntry(tw, name, body); err != nil {
			return err
		}
	}

	// Write .MANIFEST last (so size is known)
	var mBuf bytes.Buffer
	meta.EncodeTOML(&mBuf)
	if err := writeEntry(tw, ".MANIFEST", mBuf.Bytes()); err != nil {
		return err
	}

	if err := tw.Close(); err != nil {
		return err
	}
	return enc.Close()
}

func writeEntry(tw *tar.Writer, name string, data []byte) error {
	hdr := &tar.Header{
		Name: name,
		Mode: 0644,
		Size: int64(len(data)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
}

// SHA256File computes the hex sha256 of a file.
func SHA256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
