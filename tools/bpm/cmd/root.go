package cmd

import (
	"blueberry.linux/bpm/internal/config"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	cfg     *config.Config
	rootDir string
	verbose bool
)

var rootCmd = &cobra.Command{
	Use:   "bpm",
	Short: "Blueberry Package Manager",
	Long: `bpm — the Blueberry Linux package manager

Manages binary .bb packages: install, remove, upgrade, search, build.`,
	SilenceUsage: true,
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVarP(&rootDir, "root", "r", "", "install root (overrides config)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")

	rootCmd.AddCommand(
		installCmd,
		removeCmd,
		updateCmd,
		upgradeCmd,
		searchCmd,
		infoCmd,
		listCmd,
		cleanCmd,
		verifyCmd,
		buildCmd,
		repoCmd,
	)
}

func initConfig() {
	cfg = config.Default()
	if rootDir != "" {
		cfg.Root = rootDir
	}
}

func checkRoot() {
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "error: bpm requires root privileges")
		os.Exit(1)
	}
}
