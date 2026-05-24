package cmd

import (
	"blueberry.linux/bpm/internal/db"
	"blueberry.linux/bpm/internal/repo"
	"blueberry.linux/bpm/internal/solver"
	"fmt"

	"github.com/spf13/cobra"
)

var upgradeCmd = &cobra.Command{
	Use:     "upgrade [package]...",
	Aliases: []string{"up"},
	Short:   "Upgrade installed packages (all if none specified)",
	RunE:    runUpgrade,
}

var upgradeNoConfirm bool

func init() {
	upgradeCmd.Flags().BoolVarP(&upgradeNoConfirm, "yes", "y", false, "skip confirmation prompt")
}

func runUpgrade(cmd *cobra.Command, args []string) error {
	checkRoot()

	database, err := db.New(cfg.Root, cfg.DBPath)
	if err != nil {
		return err
	}

	mgr := repo.NewManager(cfg.ReposDir, cfg.CacheDir+"/indices")
	res := solver.New(database, mgr)

	plan, err := res.ResolveUpgrade(args)
	if err != nil {
		return err
	}

	if len(plan.Upgrade) == 0 {
		fmt.Println("All packages are up to date.")
		return nil
	}

	fmt.Println("The following packages will be upgraded:")
	for _, pkg := range plan.Upgrade {
		installed, _ := database.Get(pkg.Name)
		if installed != nil {
			fmt.Printf("  %s  %s -> %s\n", pkg.Name, installed.FullVersion(), pkg.FullVersion())
		} else {
			fmt.Printf("  %s-%s\n", pkg.Name, pkg.FullVersion())
		}
	}

	if !upgradeNoConfirm {
		if !confirm("Proceed?") {
			fmt.Println("Aborted.")
			return nil
		}
	}

	for _, pkg := range plan.Upgrade {
		_, repoConf, err := mgr.Find(pkg.Name)
		if err != nil {
			return err
		}
		path, err := mgr.Download(pkg, repoConf)
		if err != nil {
			return err
		}
		// Remove old version first
		if err := removePackage(database, pkg.Name); err != nil {
			fmt.Printf("warn: could not remove old %s: %v\n", pkg.Name, err)
		}
		if err := installPackageFile(database, path); err != nil {
			return fmt.Errorf("upgrade %s: %w", pkg.Name, err)
		}
	}
	return nil
}
