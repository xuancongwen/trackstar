// Package config loads and validates the environment-driven configuration.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	DriverSQLite   = "sqlite"
	DriverPostgres = "postgres"

	minSecretLength = 32
)

type Config struct {
	Addr              string
	DataDir           string
	DatabaseDriver    string
	DatabaseURL       string
	PublicURL         *url.URL
	AllowRegistration bool
	SessionSecret     []byte
	LogLevel          slog.Level
	TrustedProxies    []netip.Prefix
	Location          *time.Location
}

// SecureCookies reports whether cookies must carry the Secure attribute.
func (c *Config) SecureCookies() bool { return c.PublicURL.Scheme == "https" }

// Load reads TRACKSTAR_* variables through getenv (os.Getenv in production).
// Every problem is reported at once so a broken deployment fails loudly.
func Load(getenv func(string) string) (*Config, error) {
	var errs []error
	get := func(key, def string) string {
		if v := strings.TrimSpace(getenv(key)); v != "" {
			return v
		}
		return def
	}

	c := &Config{
		Addr:           get("TRACKSTAR_ADDR", "127.0.0.1:3000"),
		DataDir:        get("TRACKSTAR_DATA_DIR", "./data"),
		DatabaseDriver: get("TRACKSTAR_DATABASE_DRIVER", DriverSQLite),
	}

	if _, _, err := net.SplitHostPort(c.Addr); err != nil {
		errs = append(errs, fmt.Errorf("TRACKSTAR_ADDR %q: %w", c.Addr, err))
	}

	switch c.DatabaseDriver {
	case DriverSQLite:
		c.DatabaseURL = get("TRACKSTAR_DATABASE_URL", filepath.Join(c.DataDir, "trackstar.db"))
	case DriverPostgres:
		errs = append(errs, errors.New("TRACKSTAR_DATABASE_DRIVER=postgres is reserved but not implemented yet; use sqlite"))
	default:
		errs = append(errs, fmt.Errorf("TRACKSTAR_DATABASE_DRIVER %q: must be %q", c.DatabaseDriver, DriverSQLite))
	}

	_, port, _ := net.SplitHostPort(c.Addr)
	rawURL := get("TRACKSTAR_PUBLIC_URL", "http://localhost:"+port+"/")
	if u, err := url.Parse(rawURL); err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		errs = append(errs, fmt.Errorf("TRACKSTAR_PUBLIC_URL %q: must be an absolute http(s) URL", rawURL))
	} else {
		c.PublicURL = u
	}

	allow, err := strconv.ParseBool(get("TRACKSTAR_ALLOW_REGISTRATION", "true"))
	if err != nil {
		errs = append(errs, fmt.Errorf("TRACKSTAR_ALLOW_REGISTRATION: %w", err))
	}
	c.AllowRegistration = allow

	if err := c.LogLevel.UnmarshalText([]byte(get("TRACKSTAR_LOG_LEVEL", "info"))); err != nil {
		errs = append(errs, fmt.Errorf("TRACKSTAR_LOG_LEVEL: %w", err))
	}

	for _, part := range strings.Split(get("TRACKSTAR_TRUSTED_PROXIES", "127.0.0.0/8,::1/128"), ",") {
		part = strings.TrimSpace(part)
		if part == "" || part == "none" {
			continue
		}
		prefix, err := netip.ParsePrefix(part)
		if err != nil {
			if addr, aerr := netip.ParseAddr(part); aerr == nil {
				prefix, err = netip.PrefixFrom(addr, addr.BitLen()), nil
			}
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("TRACKSTAR_TRUSTED_PROXIES %q: %w", part, err))
			continue
		}
		c.TrustedProxies = append(c.TrustedProxies, prefix.Masked())
	}

	tz := get("TRACKSTAR_TIMEZONE", "UTC")
	if loc, err := time.LoadLocation(tz); err != nil {
		errs = append(errs, fmt.Errorf("TRACKSTAR_TIMEZONE %q: %w", tz, err))
	} else {
		c.Location = loc
	}

	if secret := get("TRACKSTAR_SESSION_SECRET", ""); secret != "" {
		if len(secret) < minSecretLength {
			errs = append(errs, fmt.Errorf("TRACKSTAR_SESSION_SECRET: must be at least %d characters", minSecretLength))
		}
		c.SessionSecret = []byte(secret)
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("invalid configuration:\n  %w", joinLines(errs))
	}
	return c, nil
}

// Prepare creates the data directory and, when no session secret was
// configured, loads or generates one inside it.
func (c *Config) Prepare() error {
	if err := os.MkdirAll(c.DataDir, 0o750); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	if c.DatabaseDriver == DriverSQLite {
		if err := os.MkdirAll(filepath.Dir(c.DatabaseURL), 0o750); err != nil {
			return fmt.Errorf("create database dir: %w", err)
		}
	}
	if len(c.SessionSecret) > 0 {
		return nil
	}

	path := filepath.Join(c.DataDir, "session_secret")
	data, err := os.ReadFile(path)
	if err == nil && len(strings.TrimSpace(string(data))) >= minSecretLength {
		c.SessionSecret = []byte(strings.TrimSpace(string(data)))
		return nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read session secret: %w", err)
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return err
	}
	secret := hex.EncodeToString(raw)
	if err := os.WriteFile(path, []byte(secret+"\n"), 0o600); err != nil {
		return fmt.Errorf("write session secret: %w", err)
	}
	c.SessionSecret = []byte(secret)
	return nil
}

func joinLines(errs []error) error {
	msgs := make([]string, len(errs))
	for i, e := range errs {
		msgs[i] = e.Error()
	}
	return errors.New(strings.Join(msgs, "\n  "))
}
