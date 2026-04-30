package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/jfisher/kvc/internal/config"
	"github.com/jfisher/kvc/internal/tty"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Set up ~/.config/kvc/config.toml",
	RunE:  runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
}

func runInit(_ *cobra.Command, _ []string) error {
	addr, err := tty.Prompt("Vault address (e.g. https://vault.example.com:8200): ")
	if err != nil {
		return err
	}
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return fmt.Errorf("vault address required")
	}

	user, err := tty.Prompt("Vault userpass username: ")
	if err != nil {
		return err
	}
	user = strings.TrimSpace(user)
	if user == "" {
		return fmt.Errorf("username required")
	}

	cfg := &config.Config{
		VaultAddr:     addr,
		Username:      user,
		CacheTokens:   true,
		KeyringMaxTTL: "8h",
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "wrote %s\n", config.Path())
	return nil
}
