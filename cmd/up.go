package cmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/fish-not-phish/kvc/internal/compose"
	"github.com/fish-not-phish/kvc/internal/config"
	"github.com/fish-not-phish/kvc/internal/vault"
	"github.com/spf13/cobra"
)

var (
	upFile          string
	upEnvFile       string
	upNoEnvFile     bool
	upNoCache       bool
	upPasswordStdin bool
	upVaultAddr     string
	upUsername      string
	upForceRecreate bool
	upNoRecreate    bool
	upRemoveOrphans bool
	upToken         string
	upRoleID        string
	upSecretIDStdin bool
)

var upCmd = &cobra.Command{
	Use:   "up [-- DOCKER_COMPOSE_ARGS...]",
	Short: "Auth, fetch secrets, substitute, and pipe to docker compose up -d",
	RunE:  runUp,
}

func init() {
	upCmd.Flags().StringVarP(&upFile, "file", "f", "", "compose file path (default: auto-detect compose.yaml/.yml or docker-compose.yaml/.yml in cwd)")
	upCmd.Flags().StringVar(&upEnvFile, "env-file", "", "path to .env (default: ./.env next to compose file, if present)")
	upCmd.Flags().BoolVar(&upNoEnvFile, "no-env-file", false, "skip .env auto-detection entirely")
	upCmd.Flags().BoolVar(&upNoCache, "no-cache", false, "skip kernel-keyring token cache")
	upCmd.Flags().BoolVar(&upPasswordStdin, "password-stdin", false, "read vault password from stdin instead of prompting (implies --no-cache)")
	upCmd.Flags().StringVar(&upVaultAddr, "vault-addr", "", "vault address, overrides config")
	upCmd.Flags().StringVar(&upUsername, "username", "", "vault username, overrides config")
	upCmd.Flags().BoolVar(&upForceRecreate, "force-recreate", false, "force recreate containers even if config hasn't changed")
	upCmd.Flags().BoolVar(&upNoRecreate, "no-recreate", false, "don't recreate containers if their config hasn't changed")
	upCmd.Flags().BoolVar(&upRemoveOrphans, "remove-orphans", false, "remove containers for services not defined in the compose file")
	upCmd.Flags().StringVar(&upToken, "token", "", "vault token (skips userpass auth; VAULT_TOKEN env is also accepted)")
	upCmd.Flags().StringVar(&upRoleID, "role-id", "", "vault AppRole role ID (VAULT_ROLE_ID env or role_id config also accepted)")
	upCmd.Flags().BoolVar(&upSecretIDStdin, "secret-id-stdin", false, "read AppRole secret ID from stdin (implies --no-cache)")
	rootCmd.AddCommand(upCmd)
}

func runUp(_ *cobra.Command, passthroughArgs []string) error {
	cfg, err := config.LoadOptional()
	if err != nil {
		return err
	}
	if upVaultAddr != "" {
		cfg.VaultAddr = upVaultAddr
	}
	if upUsername != "" {
		cfg.Username = upUsername
	}
	if cfg.VaultAddr == "" {
		return fmt.Errorf("vault address required: set vault_addr in config or pass --vault-addr")
	}
	usingToken := upToken != "" || os.Getenv("VAULT_TOKEN") != ""
	usingAppRole := upRoleID != "" || os.Getenv("VAULT_ROLE_ID") != "" || cfg.RoleID != ""
	if !usingToken && !usingAppRole && cfg.Username == "" {
		return fmt.Errorf("vault username required: set username in config or pass --username")
	}

	upFile, err = resolveComposeFile(upFile)
	if err != nil {
		return err
	}

	rawCompose, err := os.ReadFile(upFile)
	if err != nil {
		return fmt.Errorf("read compose file: %w", err)
	}

	envPath, err := resolveEnvFile(upFile, upEnvFile, upNoEnvFile)
	if err != nil {
		return err
	}
	envEntries, err := loadEnvEntries(envPath)
	if err != nil {
		return err
	}

	specs := allPlaceholderSpecs(rawCompose, envEntries)

	var rendered []byte
	var extraEnv []string
	if len(specs) == 0 {
		rendered = rawCompose
		// no placeholders — but still pass through .env values verbatim
		for _, e := range envEntries {
			extraEnv = append(extraEnv, e.Key+"="+e.Value)
		}
	} else {
		client, err := vault.Login(cfg, vault.LoginOpts{
			NoCache:       upNoCache,
			PasswordStdin: upPasswordStdin,
			Token:         upToken,
			RoleID:        upRoleID,
			SecretIDStdin: upSecretIDStdin,
		})
		if err != nil {
			return err
		}
		secrets, err := client.FetchAll(specs)
		if err != nil {
			return err
		}

		out, missingYAML := compose.Substitute(rawCompose, secrets)
		if len(missingYAML) > 0 {
			return fmt.Errorf("unresolved placeholders in %s: %v", upFile, missingYAML)
		}
		rendered = out

		envVars, missingEnv := resolveEnvEntries(envEntries, secrets)
		if len(missingEnv) > 0 {
			return fmt.Errorf("unresolved placeholders in %s: %v", envPath, missingEnv)
		}
		extraEnv = envVars

		// best-effort: drop secret strings from the map
		for k := range secrets {
			secrets[k] = ""
		}
	}

	upArgs := []string{"up", "-d"}
	if upForceRecreate {
		upArgs = append(upArgs, "--force-recreate")
	}
	if upNoRecreate {
		upArgs = append(upArgs, "--no-recreate")
	}
	if upRemoveOrphans {
		upArgs = append(upArgs, "--remove-orphans")
	}
	upArgs = append(upArgs, passthroughArgs...)
	return runDockerCompose(upFile, rendered, extraEnv, upArgs...)
}

func runDockerCompose(composeFile string, yaml []byte, extraEnv []string, args ...string) error {
	// Pin --project-name and --project-directory to what docker compose
	// would derive from `-f <composeFile>` directly. Without this, piping
	// via `-f -` causes docker to fall back to the cwd basename for the
	// project name and the cwd itself for relative-path resolution — so
	// `docker compose -f test/docker-compose.yml down` ends up looking under a
	// different project namespace than `kvc up -f test/docker-compose.yml`
	// created, and any relative paths inside the compose file (build
	// contexts, host bind mounts) resolve against the wrong directory.
	absCompose, err := filepath.Abs(composeFile)
	if err != nil {
		return fmt.Errorf("resolve compose path: %w", err)
	}
	projectDir := filepath.Dir(absCompose)
	projectName := filepath.Base(projectDir)

	// `--env-file /dev/null` keeps docker from auto-loading the on-disk
	// .env (which may still contain `@@…@@` placeholders). All env vars
	// come from our subprocess env, where the resolved values live.
	full := append([]string{
		"compose",
		"--project-name", projectName,
		"--project-directory", projectDir,
		"--env-file", "/dev/null",
		"-f", "-",
	}, args...)
	c := exec.Command("docker", full...)
	c.Stdin = bytes.NewReader(yaml)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if len(extraEnv) > 0 {
		c.Env = append(os.Environ(), extraEnv...)
	}
	err = c.Run()
	for i := range yaml {
		yaml[i] = 0
	}
	for i := range extraEnv {
		extraEnv[i] = ""
	}
	return err
}
