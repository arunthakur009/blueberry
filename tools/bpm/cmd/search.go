package cmd

import (
	"blueberry.linux/bpm/internal/repo"
	"fmt"

	"github.com/spf13/cobra"
)

var searchCmd = &cobra.Command{
	Use:     "search <query>",
	Aliases: []string{"s", "find"},
	Short:   "Search available packages",
	Args:    cobra.ExactArgs(1),
	RunE:    runSearch,
}

func runSearch(_ *cobra.Command, args []string) error {
	mgr := repo.NewManager(cfg.ReposDir, cfg.CacheDir+"/indices")
	results, err := mgr.Search(args[0])
	if err != nil {
		return err
	}
	if len(results) == 0 {
		fmt.Printf("No packages matching %q.\n", args[0])
		return nil
	}
	for _, p := range results {
		fmt.Printf("%-30s %-15s %s\n", p.Name, p.FullVersion(), p.Description)
	}
	return nil
}
