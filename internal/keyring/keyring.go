package keyring

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// CacheStatus describes the state of a cached token for a given vault address.
type CacheStatus struct {
	Cached    bool
	Permanent bool          // no expiry set
	Remaining time.Duration // meaningful only when Cached && !Permanent
}

// Status returns the cache state for vaultAddr without reading the token value.
func Status(vaultAddr string) CacheStatus {
	id, err := find(vaultAddr)
	if err != nil {
		return CacheStatus{}
	}
	rem, perm := readTTL(id)
	return CacheStatus{Cached: true, Permanent: perm, Remaining: rem}
}

func readTTL(id int) (rem time.Duration, perm bool) {
	data, err := os.ReadFile("/proc/keys")
	if err != nil {
		return 0, true
	}
	target := fmt.Sprintf("%08x", id)
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, target) {
			continue
		}
		// /proc/keys columns: serial flags usage timeout perms uid gid type desc
		fields := strings.Fields(line)
		if len(fields) < 4 {
			return 0, true
		}
		if fields[3] == "perm" {
			return 0, true
		}
		d, err := parseKeyTimeout(fields[3])
		if err != nil {
			return 0, true
		}
		return d, false
	}
	return 0, true
}

func parseKeyTimeout(s string) (time.Duration, error) {
	if len(s) < 2 {
		return 0, fmt.Errorf("invalid timeout %q", s)
	}
	val, err := strconv.Atoi(s[:len(s)-1])
	if err != nil {
		return 0, err
	}
	switch s[len(s)-1] {
	case 's':
		return time.Duration(val) * time.Second, nil
	case 'm':
		return time.Duration(val) * time.Minute, nil
	case 'h':
		return time.Duration(val) * time.Hour, nil
	case 'd':
		return time.Duration(val) * 24 * time.Hour, nil
	case 'w':
		return time.Duration(val) * 7 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unknown unit %c in %q", s[len(s)-1], s)
	}
}

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
