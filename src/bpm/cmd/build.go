package cmd

import (
	"blueberry.linux/bpm/internal/build"
	"fmt"

	"github.com/spf13/cobra"
)

var buildCmd = &cobra.Command{
	Use:   "build <BBUILD>",
	Short: "Build a .bb package from a BBUILD recipe",
	Args:  cobra.ExactArgs(1),
	RunE:  runBuild,
}

var (
	buildOutput  string
	buildWorkDir string
	buildJobs    int
	buildArch    string
)

func init() {
	buildCmd.Flags().StringVarP(&buildOutput, "output", "o", ".", "output directory for .bb file")
	buildCmd.Flags().StringVar(&buildWorkDir, "workdir", "", "build workspace (temp dir if empty)")
	buildCmd.Flags().IntVarP(&buildJobs, "jobs", "j", 0, "parallel make jobs (default: nproc)")
	buildCmd.Flags().StringVar(&buildArch, "arch", "", "target architecture")
}

func runBuild(_ *cobra.Command, args []string) error {
	recipe, err := build.Parse(args[0])
	if err != nil {
		return fmt.Errorf("parse BBUILD: %w", err)
	}

	fmt.Printf("Building %s-%s-%d...\n", recipe.Name, recipe.Version, recipe.Release)

	path, err := build.Build(recipe, build.BuildOptions{
		WorkDir: buildWorkDir,
		Arch:    buildArch,
		Jobs:    buildJobs,
		Output:  buildOutput,
	})
	if err != nil {
		return err
	}

	fmt.Printf("Package written to %s\n", path)
	return nil
}
