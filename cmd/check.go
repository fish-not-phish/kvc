package cmd

import (
	"fmt"
	"os"

	"github.com/jfisher/kvc/internal/compose"
	"github.com/jfisher/kvc/internal/config"
	"github.com/jfisher/kvc/internal/vault"
	"github.com/spf13/cobra"
)

var (
	checkFile       string
	checkEnvFile    string
	checkNoEnvFile  bool
	checkNoCache    bool
	checkTokenStdin bool
)

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Verify every @@<mount>/<path>#<key>@@ placeholder resolves in Vault",
	RunE:  runCheck,
}

func init() {
	checkCmd.Flags().StringVarP(&checkFile, "file", "f", "", "compose file path (default: auto-detect compose.yaml/.yml or docker-compose.yaml/.yml in cwd)")
	checkCmd.Flags().StringVar(&checkEnvFile, "env-file", "", "path to .env (default: ./.env next to compose file, if present)")
	checkCmd.Flags().BoolVar(&checkNoEnvFile, "no-env-file", false, "skip .env auto-detection entirely")
	checkCmd.Flags().BoolVar(&checkNoCache, "no-cache", false, "skip kernel-keyring token cache")
	checkCmd.Flags().BoolVar(&checkTokenStdin, "token-stdin", false, "read vault token from stdin")
	rootCmd.AddCommand(checkCmd)
}

func runCheck(_ *cobra.Command, _ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	checkFile, err = resolveComposeFile(checkFile)
	if err != nil {
		return err
	}

	rawCompose, err := os.ReadFile(checkFile)
	if err != nil {
		return err
	}

	envPath, err := resolveEnvFile(checkFile, checkEnvFile, checkNoEnvFile)
	if err != nil {
		return err
	}
	envEntries, err := loadEnvEntries(envPath)
	if err != nil {
		return err
	}

	specs := allPlaceholderSpecs(rawCompose, envEntries)
	if len(specs) == 0 {
		if envPath != "" {
			fmt.Printf("no @@<mount>/<path>#<key>@@ placeholders found in %s or %s\n", checkFile, envPath)
		} else {
			fmt.Println("no @@<mount>/<path>#<key>@@ placeholders found")
		}
		return nil
	}

	if unquoted := compose.FindUnquoted(rawCompose); len(unquoted) > 0 {
		fmt.Fprintln(os.Stderr, "warning: unquoted placeholders found in compose file — wrap them in double quotes so the file is valid YAML at rest")
		fmt.Fprintln(os.Stderr, "         (otherwise `docker compose config`, `yamllint`, IDEs, and `docker compose down -f <file>` will reject it)")
		for _, p := range unquoted {
			fmt.Fprintf(os.Stderr, "         - @@%s@@  →  \"@@%s@@\"\n", p, p)
		}
	}

	client, err := vault.Login(cfg, vault.LoginOpts{
		NoCache:    checkNoCache,
		TokenStdin: checkTokenStdin,
	})
	if err != nil {
		return err
	}

	if envPath != "" {
		fmt.Printf("(scanning %s and %s)\n", checkFile, envPath)
	}

	var missing []string
	for _, p := range specs {
		if _, err := client.Get(p); err != nil {
			fmt.Printf("MISSING %s: %v\n", p, err)
			missing = append(missing, p)
			continue
		}
		fmt.Printf("OK      %s\n", p)
	}
	if len(missing) > 0 {
		return fmt.Errorf("%d secret(s) missing or unreadable", len(missing))
	}
	return nil
}
