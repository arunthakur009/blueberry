package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Remove cached package files",
	Args:  cobra.NoArgs,
	RunE:  runClean,
}

var cleanAll bool

func init() {
	cleanCmd.Flags().BoolVarP(&cleanAll, "all", "a", false, "also remove cached indices")
}

func runClean(_ *cobra.Command, _ []string) error {
	pkgCache := filepath.Join(cfg.CacheDir, "packages")
	n, size, err := removeDir(pkgCache)
	if err != nil {
		return err
	}
	fmt.Printf("Removed %d cached packages (%.1f MB)\n", n, float64(size)/1024/1024)

	if cleanAll {
		idxCache := filepath.Join(cfg.CacheDir, "indices")
		n, size, err = removeDir(idxCache)
		if err != nil {
			return err
		}
		fmt.Printf("Removed %d cached indices (%.1f KB)\n", n, float64(size)/1024)
	}
	return nil
}

func removeDir(dir string) (count int, totalBytes int64, err error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	for _, e := range entries {
		info, _ := e.Info()
		if info != nil {
			totalBytes += info.Size()
		}
		os.Remove(filepath.Join(dir, e.Name()))
		count++
	}
	return count, totalBytes, nil
}
