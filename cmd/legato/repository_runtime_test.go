package main

import (
	"testing"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/service"
)

func TestJobClonesAreKeptIdleAsConfigured(t *testing.T) {
	for _, tc := range []struct {
		keepIdle string
		want     time.Duration
	}{{"", 7 * 24 * time.Hour}, {"2d", 48 * time.Hour}, {"0", service.KeepIdleForever}} {
		cfg := &config.Config{RepositoryCache: config.RepositoryCacheConfig{KeepIdle: tc.keepIdle}}
		if got := jobCloneKeepIdle(cfg); got != tc.want {
			t.Errorf("keep_idle %q = %v, want %v", tc.keepIdle, got, tc.want)
		}
	}
}
