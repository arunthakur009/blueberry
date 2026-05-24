package cmd

import (
	"blueberry.linux/bpm/internal/db"
	"blueberry.linux/bpm/internal/repo"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var infoCmd = &cobra.Command{
	Use:     "info <package>",
	Aliases: []string{"show"},
	Short:   "Show package information",
	Args:    cobra.ExactArgs(1),
	RunE:    runInfo,
}

func runInfo(_ *cobra.Command, args []string) error {
	name := args[0]

	database, err := db.New(cfg.Root, cfg.DBPath)
	if err != nil {
		return err
	}

	mgr := repo.NewManager(cfg.ReposDir, cfg.CacheDir+"/indices")

	// Try installed first
	if database.IsInstalled(name) {
		pkg, err := database.Get(name)
		if err != nil {
			return err
		}
		printInfo("installed", pkg.Name, pkg.FullVersion(), pkg.Arch,
			pkg.Description, pkg.URL, pkg.License,
			strings.Join(pkg.Depends, " "), pkg.Packager, pkg.InstalledSize)
	}

	// Also show repo version if available
	repoPkg, _, err := mgr.Find(name)
	if err == nil {
		status := "available"
		if database.IsInstalled(name) {
			status = "repo"
		}
		printInfo(status, repoPkg.Name, repoPkg.FullVersion(), repoPkg.Arch,
			repoPkg.Description, repoPkg.URL, repoPkg.License,
			strings.Join(repoPkg.Depends, " "), repoPkg.Packager, repoPkg.InstalledSize)
	} else if !database.IsInstalled(name) {
		return fmt.Errorf("package %q not found", name)
	}
	return nil
}

func printInfo(status, name, version, arch, desc, url, license, depends, packager string, size int64) {
	fmt.Printf("Name         : %s\n", name)
	fmt.Printf("Version      : %s\n", version)
	fmt.Printf("Architecture : %s\n", arch)
	fmt.Printf("Status       : %s\n", status)
	fmt.Printf("Description  : %s\n", desc)
	fmt.Printf("URL          : %s\n", url)
	fmt.Printf("License      : %s\n", license)
	if depends != "" {
		fmt.Printf("Depends on   : %s\n", depends)
	}
	fmt.Printf("Installed    : %.1f KB\n", float64(size)/1024)
	fmt.Printf("Packager     : %s\n", packager)
	fmt.Println()
}
