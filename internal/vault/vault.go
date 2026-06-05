package vault

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	vaultapi "github.com/hashicorp/vault/api"
	"github.com/fish-not-phish/kvc/internal/config"
	"github.com/fish-not-phish/kvc/internal/keyring"
	"github.com/fish-not-phish/kvc/internal/tty"
)

type Client struct {
	api *vaultapi.Client
}

type LoginOpts struct {
	NoCache       bool
	PasswordStdin bool
	Token         string // skip userpass entirely; also honoured via VAULT_TOKEN env
	RoleID        string // AppRole role ID; also honoured via VAULT_ROLE_ID env and config.RoleID
	SecretIDStdin bool   // read AppRole secret ID from stdin (implies no keyring write)
}

func newAPI(addr string) (*vaultapi.Client, error) {
	cfg := vaultapi.DefaultConfig()
	cfg.Address = addr
	cfg.Timeout = 30 * time.Second
	return vaultapi.NewClient(cfg)
}

func Login(cfg *config.Config, opts LoginOpts) (*Client, error) {
	c, err := newAPI(cfg.VaultAddr)
	if err != nil {
		return nil, err
	}

	// 1. Direct token: --token flag, then VAULT_TOKEN env.
	tok := opts.Token
	if tok == "" {
		tok = os.Getenv("VAULT_TOKEN")
	}
	if tok != "" {
		c.SetToken(tok)
		return &Client{api: c}, nil
	}

	// 2. Userpass via stdin — no keyring read or write.
	if opts.PasswordStdin {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, err
		}
		defer zero(b)
		pw := strings.TrimRight(string(b), "\r\n")
		if pw == "" {
			return nil, fmt.Errorf("--password-stdin: empty input")
		}
		secret, err := c.Logical().Write(
			"auth/userpass/login/"+cfg.Username,
			map[string]interface{}{"password": pw},
		)
		if err != nil {
			return nil, fmt.Errorf("userpass login: %w", err)
		}
		if secret == nil || secret.Auth == nil || secret.Auth.ClientToken == "" {
			return nil, fmt.Errorf("userpass login: empty auth response")
		}
		c.SetToken(secret.Auth.ClientToken)
		return &Client{api: c}, nil
	}

	// 3. AppRole via stdin — no keyring read or write.
	if opts.SecretIDStdin {
		roleID := resolveRoleID(opts, cfg)
		if roleID == "" {
			return nil, fmt.Errorf("--secret-id-stdin requires a role ID: use --role-id or set role_id in config")
		}
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, err
		}
		defer zero(b)
		secretID := strings.TrimRight(string(b), "\r\n")
		if secretID == "" {
			return nil, fmt.Errorf("--secret-id-stdin: empty input")
		}
		secret, err := approleAuth(c, roleID, secretID)
		if err != nil {
			return nil, err
		}
		c.SetToken(secret.Auth.ClientToken)
		return &Client{api: c}, nil
	}

	// 4. Keyring cache (both userpass and AppRole honour this).
	cacheOK := cfg.CacheTokens && !opts.NoCache
	if cacheOK {
		if tok, err := keyring.Get(cfg.VaultAddr); err == nil && tok != "" {
			c.SetToken(tok)
			return &Client{api: c}, nil
		}
	}

	// 5. AppRole via VAULT_SECRET_ID env.
	if roleID := resolveRoleID(opts, cfg); roleID != "" {
		secretID := os.Getenv("VAULT_SECRET_ID")
		if secretID == "" {
			return nil, fmt.Errorf("AppRole role ID set but no secret ID found: use --secret-id-stdin or set VAULT_SECRET_ID")
		}
		secret, err := approleAuth(c, roleID, secretID)
		if err != nil {
			return nil, err
		}
		c.SetToken(secret.Auth.ClientToken)
		if cacheOK {
			cacheToken(cfg, secret)
		}
		return &Client{api: c}, nil
	}

	// 6. Userpass interactive.
	pw, err := tty.PromptPassword(fmt.Sprintf("Vault password for %s: ", cfg.Username))
	if err != nil {
		return nil, err
	}
	defer zero(pw)

	secret, err := c.Logical().Write(
		"auth/userpass/login/"+cfg.Username,
		map[string]interface{}{"password": string(pw)},
	)
	if err != nil {
		return nil, fmt.Errorf("userpass login: %w", err)
	}
	if secret == nil || secret.Auth == nil || secret.Auth.ClientToken == "" {
		return nil, fmt.Errorf("userpass login: empty auth response")
	}
	c.SetToken(secret.Auth.ClientToken)

	if cacheOK {
		cacheToken(cfg, secret)
	}

	return &Client{api: c}, nil
}

func resolveRoleID(opts LoginOpts, cfg *config.Config) string {
	if opts.RoleID != "" {
		return opts.RoleID
	}
	if v := os.Getenv("VAULT_ROLE_ID"); v != "" {
		return v
	}
	return cfg.RoleID
}

func approleAuth(c *vaultapi.Client, roleID, secretID string) (*vaultapi.Secret, error) {
	secret, err := c.Logical().Write("auth/approle/login", map[string]interface{}{
		"role_id":   roleID,
		"secret_id": secretID,
	})
	if err != nil {
		return nil, fmt.Errorf("approle login: %w", err)
	}
	if secret == nil || secret.Auth == nil || secret.Auth.ClientToken == "" {
		return nil, fmt.Errorf("approle login: empty auth response")
	}
	return secret, nil
}

func cacheToken(cfg *config.Config, secret *vaultapi.Secret) {
	ttl := time.Duration(secret.Auth.LeaseDuration) * time.Second
	if ttl == 0 || ttl > cfg.MaxTTL() {
		ttl = cfg.MaxTTL()
	}
	if err := keyring.Set(cfg.VaultAddr, secret.Auth.ClientToken, ttl); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to cache token in keyring: %v\n", err)
	}
}

// ParseSpec splits a placeholder spec of shape `<mount>/<path>#<key>` into
// its three parts. Exposed so callers (e.g. `kvc check`) can validate
// placeholders before any Vault round-trip.
func ParseSpec(spec string) (mount, path, key string, err error) {
	hash := strings.Index(spec, "#")
	if hash < 0 {
		return "", "", "", fmt.Errorf("invalid spec %q: expected <mount>/<path>#<key>", spec)
	}
	key = spec[hash+1:]
	spec = spec[:hash]
	if key == "" {
		return "", "", "", fmt.Errorf("invalid spec: empty key after #")
	}
	slash := strings.Index(spec, "/")
	if slash <= 0 || slash >= len(spec)-1 {
		return "", "", "", fmt.Errorf("invalid spec: expected <mount>/<path>#<key>")
	}
	mount = spec[:slash]
	path = spec[slash+1:]
	return mount, path, key, nil
}

func (c *Client) Get(spec string) (string, error) {
	mount, path, key, err := ParseSpec(spec)
	if err != nil {
		return "", err
	}
	full := fmt.Sprintf("%s/data/%s", mount, path)
	secret, err := c.api.Logical().ReadWithContext(context.Background(), full)
	if err != nil {
		return "", err
	}
	if secret == nil || secret.Data == nil {
		return "", fmt.Errorf("not found at %s/%s", mount, path)
	}
	data, ok := secret.Data["data"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("malformed KV v2 response (is %q a KV v2 mount?)", mount)
	}
	v, ok := data[key]
	if !ok {
		return "", fmt.Errorf("key %q not present at %s/%s", key, mount, path)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("key %q at %s/%s is not a string", key, mount, path)
	}
	return s, nil
}

func (c *Client) FetchAll(paths []string) (map[string]string, error) {
	out := make(map[string]string, len(paths))
	for _, p := range paths {
		v, err := c.Get(p)
		if err != nil {
			return nil, fmt.Errorf("vault read %s: %w", p, err)
		}
		out[p] = v
	}
	return out, nil
}

func Revoke(addr, token string) error {
	c, err := newAPI(addr)
	if err != nil {
		return err
	}
	c.SetToken(token)
	return c.Auth().Token().RevokeSelf(token)
}

// HealthInfo holds the subset of sys/health we surface in kvc status.
type HealthInfo struct {
	Version string
	Sealed  bool
	Standby bool
}

// Ping calls sys/health without authentication and returns basic vault state.
func Ping(addr string) (*HealthInfo, error) {
	c, err := newAPI(addr)
	if err != nil {
		return nil, err
	}
	h, err := c.Sys().Health()
	if err != nil {
		return nil, err
	}
	return &HealthInfo{Version: h.Version, Sealed: h.Sealed, Standby: h.Standby}, nil
}

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
