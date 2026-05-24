package cmd

import (
	"blueberry.linux/bpm/internal/db"
	"blueberry.linux/bpm/internal/repo"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List packages",
	RunE:    runList,
}

var (
	listInstalled  bool
	listAvailable  bool
	listUpgradable bool
)

func init() {
	listCmd.Flags().BoolVarP(&listInstalled, "installed", "i", true, "list installed packages")
	listCmd.Flags().BoolVarP(&listAvailable, "available", "a", false, "list all available packages")
	listCmd.Flags().BoolVarP(&listUpgradable, "upgradable", "u", false, "list upgradable packages")
}

func runList(_ *cobra.Command, _ []string) error {
	database, err := db.New(cfg.Root, cfg.DBPath)
	if err != nil {
		return err
	}

	if listAvailable {
		return listAllAvailable()
	}

	names, err := database.List()
	if err != nil {
		return err
	}
	sort.Strings(names)

	if listUpgradable {
		return listUpgradablePackages(database, names)
	}

	// Default: list installed
	world, _ := database.World()
	for _, name := range names {
		pkg, err := database.Get(name)
		if err != nil {
			continue
		}
		explicit := ""
		if world[name] {
			explicit = " [explicit]"
		}
		fmt.Printf("%-30s %-15s %s%s\n", pkg.Name, pkg.FullVersion(), pkg.Description, explicit)
	}
	return nil
}

func listAllAvailable() error {
	mgr := repo.NewManager(cfg.ReposDir, cfg.CacheDir+"/indices")
	indices, err := mgr.AllIndices()
	if err != nil {
		return err
	}
	seen := make(map[string]bool)
	var names []string
	pkgMap := make(map[string]interface{})

	for _, idx := range indices {
		for _, p := range idx.Packages {
			if !seen[p.Name] {
				seen[p.Name] = true
				names = append(names, p.Name)
				pkgMap[p.Name] = p
			}
		}
	}
	sort.Strings(names)

	for _, name := range names {
		if p, ok := pkgMap[name]; ok {
			type pkg interface {
				FullVersion() string
			}
			_ = p
			fmt.Printf("%-30s %s\n", name, strings.TrimSpace(""))
		}
	}
	return nil
}

func listUpgradablePackages(database *db.DB, names []string) error {
	mgr := repo.NewManager(cfg.ReposDir, cfg.CacheDir+"/indices")
	any := false
	for _, name := range names {
		installed, err := database.Get(name)
		if err != nil {
			continue
		}
		repoPkg, _, err := mgr.Find(name)
		if err != nil {
			continue
		}
		if repoPkg.Version != installed.Version || repoPkg.Release > installed.Release {
			fmt.Printf("%-30s %s -> %s\n", name, installed.FullVersion(), repoPkg.FullVersion())
			any = true
		}
	}
	if !any {
		fmt.Println("All packages are up to date.")
	}
	return nil
}
