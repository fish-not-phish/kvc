package cmd

import (
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

var downFile string

var downCmd = &cobra.Command{
	Use:   "down",
	Short: "Run docker compose down (no secret fetching)",
	RunE:  runDown,
}

func init() {
	downCmd.Flags().StringVarP(&downFile, "file", "f", "", "compose file path (default: auto-detect compose.yaml/.yml or docker-compose.yaml/.yml in cwd)")
	rootCmd.AddCommand(downCmd)
}

func runDown(_ *cobra.Command, _ []string) error {
	resolved, err := resolveComposeFile(downFile)
	if err != nil {
		return err
	}
	c := exec.Command("docker", "compose", "-f", resolved, "down")
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}
