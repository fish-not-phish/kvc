package cmd

import (
	"fmt"
	"os"

	"github.com/fish-not-phish/kvc/internal/config"
	"github.com/fish-not-phish/kvc/internal/keyring"
	"github.com/fish-not-phish/kvc/internal/vault"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show config, cached token state, and Vault reachability",
	RunE:  runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(_ *cobra.Command, _ []string) error {
	cfg, err := config.LoadOptional()
	if err != nil {
		return err
	}

	fmt.Println("Config:")
	if cfg.VaultAddr == "" && cfg.Username == "" {
		fmt.Printf("  (no config file found at %s)\n", config.Path())
	} else {
		fmt.Printf("  vault_addr:      %s\n", orNone(cfg.VaultAddr))
		fmt.Printf("  username:        %s\n", orNone(cfg.Username))
		fmt.Printf("  cache_tokens:    %v\n", cfg.CacheTokens)
		fmt.Printf("  keyring_max_ttl: %s\n", cfg.KeyringMaxTTL)
	}

	fmt.Println()
	fmt.Println("Keyring:")
	if cfg.VaultAddr == "" {
		fmt.Println("  (no vault_addr configured; cannot check keyring)")
	} else {
		cs := keyring.Status(cfg.VaultAddr)
		if !cs.Cached {
			fmt.Println("  token cached:    no")
		} else if cs.Permanent {
			fmt.Println("  token cached:    yes")
			fmt.Println("  expires in:      never (no TTL set)")
		} else {
			fmt.Println("  token cached:    yes")
			fmt.Printf("  expires in:      %s\n", cs.Remaining.Round(1e9))
		}
	}

	fmt.Println()
	fmt.Println("Vault:")
	addr := cfg.VaultAddr
	if addr == "" {
		fmt.Println("  (no vault_addr configured; cannot check reachability)")
		return nil
	}
	if tok := os.Getenv("VAULT_ADDR"); tok != "" {
		addr = tok
	}
	h, err := vault.Ping(addr)
	if err != nil {
		fmt.Printf("  reachable:       no (%v)\n", err)
		return nil
	}
	sealed := "no"
	if h.Sealed {
		sealed = "YES (vault is sealed)"
	}
	standby := "no"
	if h.Standby {
		standby = "yes"
	}
	fmt.Printf("  reachable:       yes\n")
	fmt.Printf("  version:         %s\n", h.Version)
	fmt.Printf("  sealed:          %s\n", sealed)
	fmt.Printf("  standby:         %s\n", standby)
	return nil
}

func orNone(s string) string {
	if s == "" {
		return "(not set)"
	}
	return s
}
