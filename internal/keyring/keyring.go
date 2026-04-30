package keyring

import (
	"fmt"
	"time"

	"golang.org/x/sys/unix"
)

const keyType = "user"

func desc(vaultAddr string) string {
	return "kvc:" + vaultAddr
}

func find(vaultAddr string) (int, error) {
	return unix.KeyctlSearch(unix.KEY_SPEC_SESSION_KEYRING, keyType, desc(vaultAddr), 0)
}

func Get(vaultAddr string) (string, error) {
	id, err := find(vaultAddr)
	if err != nil {
		return "", err
	}
	// Probe size with a nil buffer, then read.
	n, err := unix.KeyctlBuffer(unix.KEYCTL_READ, id, nil, 0)
	if err != nil {
		return "", err
	}
	if n == 0 {
		return "", nil
	}
	buf := make([]byte, n)
	got, err := unix.KeyctlBuffer(unix.KEYCTL_READ, id, buf, n)
	if err != nil {
		return "", err
	}
	if got < len(buf) {
		buf = buf[:got]
	}
	return string(buf), nil
}

func Set(vaultAddr, token string, ttl time.Duration) error {
	// Unlink any prior entry first. AddKey on a description that matches a
	// revoked-but-still-linked key fails with "key has been revoked", so we
	// defensively unlink before adding.
	if old, err := find(vaultAddr); err == nil {
		_, _ = unix.KeyctlInt(unix.KEYCTL_UNLINK, old, unix.KEY_SPEC_SESSION_KEYRING, 0, 0)
	}
	id, err := unix.AddKey(keyType, desc(vaultAddr), []byte(token), unix.KEY_SPEC_SESSION_KEYRING)
	if err != nil {
		return fmt.Errorf("add_key: %w", err)
	}
	if ttl > 0 {
		secs := int(ttl.Seconds())
		if secs < 1 {
			secs = 1
		}
		if _, err := unix.KeyctlInt(unix.KEYCTL_SET_TIMEOUT, id, secs, 0, 0); err != nil {
			return fmt.Errorf("set_timeout: %w", err)
		}
	}
	return nil
}

func Clear(vaultAddr string) error {
	id, err := find(vaultAddr)
	if err != nil {
		return nil
	}
	// Unlink (not revoke) so the same description can be re-added later.
	// The server-side token revoke is handled separately by `kvc logout`.
	if _, err := unix.KeyctlInt(unix.KEYCTL_UNLINK, id, unix.KEY_SPEC_SESSION_KEYRING, 0, 0); err != nil {
		return fmt.Errorf("unlink: %w", err)
	}
	return nil
}
