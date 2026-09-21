package config

import (
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadDefaults(t *testing.T) {
	c, err := Load(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.DatabaseDriver != DriverSQLite || !c.AllowRegistration || c.PublicURL.Host != "localhost:3000" {
		t.Fatalf("unexpected defaults: %+v", c)
	}
}

func TestLoadReportsAllProblems(t *testing.T) {
	_, err := Load(env(map[string]string{
		"TRACKER_ADDR":               "nonsense",
		"TRACKER_PUBLIC_URL":         "track.example.com",
		"TRACKER_ALLOW_REGISTRATION": "maybe",
		"TRACKER_SESSION_SECRET":     "short",
		"TRACKER_DATABASE_DRIVER":    "mysql",
	}))
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"TRACKER_ADDR", "TRACKER_PUBLIC_URL", "TRACKER_ALLOW_REGISTRATION", "TRACKER_SESSION_SECRET", "TRACKER_DATABASE_DRIVER"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %s: %v", want, err)
		}
	}
}

func TestPrepareGeneratesStableSecret(t *testing.T) {
	dir := t.TempDir()
	load := func() string {
		c, err := Load(env(map[string]string{"TRACKER_DATA_DIR": dir}))
		if err != nil {
			t.Fatal(err)
		}
		if err := c.Prepare(); err != nil {
			t.Fatal(err)
		}
		return string(c.SessionSecret)
	}
	first := load()
	if len(first) < minSecretLength {
		t.Fatalf("secret too short: %q", first)
	}
	if second := load(); second != first {
		t.Fatal("secret changed between startups")
	}
}
