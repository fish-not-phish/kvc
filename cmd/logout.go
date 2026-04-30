package cmd

import (
	"fmt"
	"os"

	"github.com/fish-not-phish/kvc/internal/config"
	"github.com/fish-not-phish/kvc/internal/keyring"
	"github.com/fish-not-phish/kvc/internal/vault"
	"github.com/spf13/cobra"
)

var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Revoke the cached vault token and clear the kernel-keyring entry",
	RunE:  runLogout,
}

func init() {
	rootCmd.AddCommand(logoutCmd)
}

func runLogout(_ *cobra.Command, _ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	tok, err := keyring.Get(cfg.VaultAddr)
	if err != nil || tok == "" {
		fmt.Fprintln(os.Stderr, "no cached token")
		return nil
	}
	if err := vault.Revoke(cfg.VaultAddr, tok); err != nil {
		fmt.Fprintf(os.Stderr, "warning: revoke failed (continuing): %v\n", err)
	}
	if err := keyring.Clear(cfg.VaultAddr); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "logged out")
	return nil
}
