package cmd

import (
	"blueberry.linux/bpm/internal/archive"
	"blueberry.linux/bpm/internal/db"
	"blueberry.linux/bpm/internal/repo"
	"blueberry.linux/bpm/internal/solver"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var removeCmd = &cobra.Command{
	Use:     "remove <package>...",
	Aliases: []string{"rm", "del"},
	Short:   "Remove installed packages",
	Args:    cobra.MinimumNArgs(1),
	RunE:    runRemove,
}

var removeNoConfirm bool

func init() {
	removeCmd.Flags().BoolVarP(&removeNoConfirm, "yes", "y", false, "skip confirmation prompt")
}

func runRemove(cmd *cobra.Command, args []string) error {
	checkRoot()

	database, err := db.New(cfg.Root, cfg.DBPath)
	if err != nil {
		return err
	}

	mgr := repo.NewManager(cfg.ReposDir, cfg.CacheDir+"/indices")
	res := solver.New(database, mgr)

	plan, err := res.ResolveRemove(args)
	if err != nil {
		return err
	}

	if len(plan.Remove) == 0 {
		fmt.Println("Nothing to remove.")
		return nil
	}

	fmt.Println("The following packages will be removed:")
	for _, pkg := range plan.Remove {
		fmt.Printf("  %s-%s\n", pkg.Name, pkg.FullVersion())
	}

	if !removeNoConfirm {
		if !confirm("Proceed?") {
			fmt.Println("Aborted.")
			return nil
		}
	}

	for _, pkg := range plan.Remove {
		if err := removePackage(database, pkg.Name); err != nil {
			return fmt.Errorf("remove %s: %w", pkg.Name, err)
		}
		if err := database.RemoveFromWorld(pkg.Name); err != nil {
			return err
		}
	}
	return nil
}

func removePackage(database *db.DB, name string) error {
	meta, err := database.Get(name)
	if err != nil {
		return err
	}

	// Run pre-remove script from cached archive if available
	cachedBB := filepath.Join(cfg.CacheDir, "packages",
		fmt.Sprintf("%s-%s-%d-%s.bb", meta.Name, meta.Version, meta.Release, meta.Arch))
	if f, err := os.Open(cachedBB); err == nil {
		if pkg, err := archive.Open(f); err == nil {
			if script := pkg.Script(archive.ScriptPreRemove); script != nil {
				runScript(script, "pre-remove", name)
			}
		}
		f.Close()
	}

	fmt.Printf("Removing %s-%s...\n", meta.Name, meta.FullVersion())

	files, err := database.Files(name)
	if err != nil {
		return err
	}

	// Remove files in reverse order (deepest first)
	for i := len(files) - 1; i >= 0; i-- {
		path := filepath.Join(cfg.Root, files[i])
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "warn: remove %s: %v\n", path, err)
		}
	}

	// Remove empty directories
	for i := len(files) - 1; i >= 0; i-- {
		dir := filepath.Dir(filepath.Join(cfg.Root, files[i]))
		os.Remove(dir) // fails silently if not empty — that's correct
	}

	if err := database.Remove(name); err != nil {
		return err
	}

	fmt.Printf("  [-] %s-%s\n", meta.Name, meta.FullVersion())
	return nil
}
