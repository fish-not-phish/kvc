package cmd

import "github.com/spf13/cobra"

var Version = "dev"

var rootCmd = &cobra.Command{
	Use:           "kvc",
	Short:         "Vault-backed secret injection for Docker Compose",
	Long:          "kvc injects secrets from a HashiCorp Vault or OpenBao server into a\nDocker Compose file at deploy time, then runs docker compose against it.\nPlaintext secrets never touch the host's disk.",
	SilenceUsage:  true,
	SilenceErrors: true,
}

func Execute() error {
	rootCmd.Version = Version
	return rootCmd.Execute()
}
