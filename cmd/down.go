package cmd

import (
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

var (
	downFile          string
	downVolumes       bool
	downRemoveOrphans bool
)

var downCmd = &cobra.Command{
	Use:   "down",
	Short: "Run docker compose down (no secret fetching)",
	RunE:  runDown,
}

func init() {
	downCmd.Flags().StringVarP(&downFile, "file", "f", "", "compose file path (default: auto-detect compose.yaml/.yml or docker-compose.yaml/.yml in cwd)")
	downCmd.Flags().BoolVarP(&downVolumes, "volumes", "v", false, "remove named volumes declared in the compose file (irreversible)")
	downCmd.Flags().BoolVar(&downRemoveOrphans, "remove-orphans", false, "remove containers for services not defined in the compose file")
	rootCmd.AddCommand(downCmd)
}

func runDown(_ *cobra.Command, _ []string) error {
	resolved, err := resolveComposeFile(downFile)
	if err != nil {
		return err
	}
	args := []string{"compose", "-f", resolved, "down"}
	if downVolumes {
		args = append(args, "--volumes")
	}
	if downRemoveOrphans {
		args = append(args, "--remove-orphans")
	}
	c := exec.Command("docker", args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}
