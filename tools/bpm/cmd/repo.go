package cmd

import (
	"blueberry.linux/bpm/internal/repo"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var repoCmd = &cobra.Command{
	Use:   "repo",
	Short: "Manage package repositories",
}

var repoListCmd = &cobra.Command{
	Use:   "list",
	Short: "List configured repositories",
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr := repo.NewManager(cfg.ReposDir, cfg.CacheDir+"/indices")
		repos, err := mgr.LoadRepos()
		if err != nil {
			return err
		}
		if len(repos) == 0 {
			fmt.Println("No repositories configured.")
			return nil
		}
		for _, r := range repos {
			enabled := "enabled"
			if !r.Enabled {
				enabled = "disabled"
			}
			fmt.Printf("%-20s %-10s %s\n", r.Name, enabled, r.URL)
		}
		return nil
	},
}

var repoAddCmd = &cobra.Command{
	Use:   "add <name> <url>",
	Short: "Add a repository",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		checkRoot()
		name, url := args[0], args[1]
		confPath := filepath.Join(cfg.ReposDir, name+".conf")
		if _, err := os.Stat(confPath); err == nil {
			return fmt.Errorf("repository %q already exists", name)
		}
		content := fmt.Sprintf("name = %q\nurl = %q\nenabled = true\n", name, url)
		if err := os.WriteFile(confPath, []byte(content), 0644); err != nil {
			return err
		}
		fmt.Printf("Added repository %q (%s)\n", name, url)
		fmt.Println("Run 'bpm update' to fetch the package index.")
		return nil
	},
}

var repoRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove a repository",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		checkRoot()
		name := args[0]
		confPath := filepath.Join(cfg.ReposDir, name+".conf")
		if err := os.Remove(confPath); err != nil {
			return fmt.Errorf("repository %q not found", name)
		}
		fmt.Printf("Removed repository %q\n", name)
		return nil
	},
}

var repoEnableCmd = &cobra.Command{
	Use:   "enable <name>",
	Short: "Enable a disabled repository",
	Args:  cobra.ExactArgs(1),
	RunE:  repoSetEnabled(true),
}

var repoDisableCmd = &cobra.Command{
	Use:   "disable <name>",
	Short: "Disable a repository without removing it",
	Args:  cobra.ExactArgs(1),
	RunE:  repoSetEnabled(false),
}

func repoSetEnabled(enabled bool) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		checkRoot()
		name := args[0]
		confPath := filepath.Join(cfg.ReposDir, name+".conf")
		data, err := os.ReadFile(confPath)
		if err != nil {
			return fmt.Errorf("repository %q not found", name)
		}
		val := "true"
		if !enabled {
			val = "false"
		}
		content := string(data)
		if strings.Contains(content, "enabled =") {
			lines := strings.Split(content, "\n")
			for i, l := range lines {
				if strings.HasPrefix(strings.TrimSpace(l), "enabled") {
					lines[i] = "enabled = " + val
				}
			}
			content = strings.Join(lines, "\n")
		} else {
			content += "\nenabled = " + val + "\n"
		}
		return os.WriteFile(confPath, []byte(content), 0644)
	}
}

func init() {
	repoCmd.AddCommand(repoListCmd, repoAddCmd, repoRemoveCmd, repoEnableCmd, repoDisableCmd)
}
