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

	cacheOK := cfg.CacheTokens && !opts.NoCache
	if cacheOK {
		if tok, err := keyring.Get(cfg.VaultAddr); err == nil && tok != "" {
			c.SetToken(tok)
			return &Client{api: c}, nil
		}
	}

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
		ttl := time.Duration(secret.Auth.LeaseDuration) * time.Second
		if ttl == 0 || ttl > cfg.MaxTTL() {
			ttl = cfg.MaxTTL()
		}
		if err := keyring.Set(cfg.VaultAddr, secret.Auth.ClientToken, ttl); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to cache token in keyring: %v\n", err)
		}
	}

	return &Client{api: c}, nil
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

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
