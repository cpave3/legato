package config

import (
	"strings"
	"testing"
	"time"
)

func TestRepositoryCacheMaxAgeDefaultsToFiveMinutes(t *testing.T) {
	writeTestConfig(t, "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.RepositoryCache.MaxAgeDuration(); got != 5*time.Minute {
		t.Fatalf("max age = %v, want 5m", got)
	}
}

func TestRepositoryCacheMaxAge(t *testing.T) {
	for _, tc := range []struct {
		yaml string
		want time.Duration
	}{{"30s", 30 * time.Second}, {"0s", 0}} {
		writeTestConfig(t, "repository_cache:\n  max_age: "+tc.yaml+"\n")
		cfg, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if got := cfg.RepositoryCache.MaxAgeDuration(); got != tc.want {
			t.Fatalf("max_age %s = %v, want %v", tc.yaml, got, tc.want)
		}
	}
}

func TestRepositoryCacheRejectsABadMaxAge(t *testing.T) {
	for _, bad := range []string{"soon", "-1m"} {
		writeTestConfig(t, "repository_cache:\n  max_age: "+bad+"\n")
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "repository_cache.max_age") {
			t.Fatalf("max_age %s: error = %v", bad, err)
		}
	}
}

func TestFreshClonesAreOfferedOnlyWhenEnabled(t *testing.T) {
	writeTestConfig(t, "")
	cfg, err := Load()
	if err != nil || cfg.RepositoryCache.Enabled {
		t.Fatalf("enabled by default: %+v, %v", cfg.RepositoryCache, err)
	}
	writeTestConfig(t, "repository_cache:\n  enabled: true\n")
	if cfg, err = Load(); err != nil || !cfg.RepositoryCache.Enabled {
		t.Fatalf("enabled: %+v, %v", cfg.RepositoryCache, err)
	}
}

func TestRepositoryCacheKeepIdle(t *testing.T) {
	for _, tc := range []struct {
		yaml    string
		want    time.Duration
		forever bool
	}{{"", 7 * 24 * time.Hour, false}, {"keep_idle: 3d", 3 * 24 * time.Hour, false}, {"keep_idle: 36h", 36 * time.Hour, false},
		{"keep_idle: 0", 0, true}, {"keep_idle: 0s", 0, true}} {
		writeTestConfig(t, "repository_cache:\n  enabled: true\n  "+tc.yaml+"\n")
		cfg, err := Load()
		if err != nil {
			t.Fatalf("%q: %v", tc.yaml, err)
		}
		got, forever := cfg.RepositoryCache.KeepIdleDuration()
		if got != tc.want || forever != tc.forever {
			t.Fatalf("%q = %v forever %v, want %v forever %v", tc.yaml, got, forever, tc.want, tc.forever)
		}
	}
}

func TestRepositoryCacheRejectsABadKeepIdle(t *testing.T) {
	for _, bad := range []string{"soon", "-1d", "-2h", "1.5d"} {
		writeTestConfig(t, "repository_cache:\n  keep_idle: "+bad+"\n")
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "repository_cache.keep_idle") {
			t.Fatalf("keep_idle %s: error = %v", bad, err)
		}
	}
}
