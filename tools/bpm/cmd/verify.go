package cmd

import (
	"blueberry.linux/bpm/internal/archive"
	"blueberry.linux/bpm/internal/db"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var verifyCmd = &cobra.Command{
	Use:   "verify [package]...",
	Short: "Verify integrity of installed packages",
	RunE:  runVerify,
}

func runVerify(_ *cobra.Command, args []string) error {
	database, err := db.New(cfg.Root, cfg.DBPath)
	if err != nil {
		return err
	}

	if len(args) == 0 {
		args, err = database.List()
		if err != nil {
			return err
		}
	}

	anyFailed := false
	for _, name := range args {
		ok, issues := verifyPackage(database, name)
		if !ok {
			anyFailed = true
			fmt.Printf("FAIL %s:\n", name)
			for _, issue := range issues {
				fmt.Printf("  %s\n", issue)
			}
		} else {
			fmt.Printf("OK   %s\n", name)
		}
	}

	if anyFailed {
		return fmt.Errorf("integrity check failed")
	}
	return nil
}

func verifyPackage(database *db.DB, name string) (bool, []string) {
	meta, err := database.Get(name)
	if err != nil {
		return false, []string{"not installed"}
	}

	// Try to use checksums from cached .bb
	cachedBB := filepath.Join(cfg.CacheDir, "packages",
		fmt.Sprintf("%s-%s-%d-%s.bb", meta.Name, meta.Version, meta.Release, meta.Arch))

	f, err := os.Open(cachedBB)
	if err != nil {
		// Fall back to just checking files exist
		return verifyFilesExist(database, name)
	}
	defer f.Close()

	pkg, err := archive.Open(f)
	if err != nil {
		return verifyFilesExist(database, name)
	}

	var issues []string
	for path, expectedHash := range pkg.Checksums {
		fullPath := filepath.Join(cfg.Root, path)
		actualHash, err := hashFileSHA256(fullPath)
		if err != nil {
			issues = append(issues, fmt.Sprintf("missing: %s", path))
			continue
		}
		if actualHash != expectedHash {
			issues = append(issues, fmt.Sprintf("modified: %s", path))
		}
	}
	return len(issues) == 0, issues
}

func verifyFilesExist(database *db.DB, name string) (bool, []string) {
	files, err := database.Files(name)
	if err != nil {
		return false, []string{err.Error()}
	}
	var issues []string
	for _, f := range files {
		if _, err := os.Lstat(filepath.Join(cfg.Root, f)); os.IsNotExist(err) {
			issues = append(issues, "missing: "+f)
		}
	}
	return len(issues) == 0, issues
}

func hashFileSHA256(path string) (string, error) {
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
