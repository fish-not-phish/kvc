package cmd

import (
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

var restartFile string

var restartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Run docker compose restart (no secret fetching)",
	RunE:  runRestart,
}

func init() {
	restartCmd.Flags().StringVarP(&restartFile, "file", "f", "", "compose file path (default: auto-detect compose.yaml/.yml or docker-compose.yaml/.yml in cwd)")
	rootCmd.AddCommand(restartCmd)
}

func runRestart(_ *cobra.Command, _ []string) error {
	resolved, err := resolveComposeFile(restartFile)
	if err != nil {
		return err
	}
	c := exec.Command("docker", "compose", "-f", resolved, "restart")
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}
