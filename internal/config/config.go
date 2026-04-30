package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

type Config struct {
	VaultAddr     string `toml:"vault_addr"`
	Username      string `toml:"username"`
	CacheTokens   bool   `toml:"cache_tokens"`
	KeyringMaxTTL string `toml:"keyring_max_ttl"`
}

func Path() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "kvc", "config.toml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "kvc", "config.toml")
}

func Load() (*Config, error) {
	p := Path()
	var c Config
	if _, err := toml.DecodeFile(p, &c); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no config at %s — run `kvc init`", p)
		}
		return nil, err
	}
	if c.VaultAddr == "" || c.Username == "" {
		return nil, fmt.Errorf("config %s missing vault_addr or username", p)
	}
	if c.KeyringMaxTTL == "" {
		c.KeyringMaxTTL = "8h"
	}
	return &c, nil
}

func (c *Config) Save() error {
	p := Path()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(c)
}

func (c *Config) MaxTTL() time.Duration {
	d, err := time.ParseDuration(c.KeyringMaxTTL)
	if err != nil {
		return 8 * time.Hour
	}
	return d
}
