package cmd

import (
	"blueberry.linux/bpm/internal/repo"
	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:     "update",
	Aliases: []string{"sync"},
	Short:   "Sync package indices from all repositories",
	Args:    cobra.NoArgs,
	RunE:    runUpdate,
}

func runUpdate(_ *cobra.Command, _ []string) error {
	mgr := repo.NewManager(cfg.ReposDir, cfg.CacheDir+"/indices")
	return mgr.Update()
}
