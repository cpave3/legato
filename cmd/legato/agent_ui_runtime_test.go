package main

import (
	"testing"

	"github.com/cpave3/legato/config"
)

// The TUI links to the web UI for what it cannot draw. The address is where
// this machine serves the web UI, or empty when it serves none.
func TestLocalWebURL(t *testing.T) {
	web := func(mutate func(*config.WebConfig)) *config.Config {
		cfg := &config.Config{Web: config.WebConfig{Enabled: true, Port: "3080"}}
		mutate(&cfg.Web)
		return cfg
	}
	cases := map[string]struct {
		cfg  *config.Config
		want string
	}{
		"web UI off":            {web(func(w *config.WebConfig) { w.Enabled = false }), ""},
		"plain HTTP":            {web(func(*config.WebConfig) {}), "http://localhost:3080"},
		"another port":          {web(func(w *config.WebConfig) { w.Port = "4000" }), "http://localhost:4000"},
		"generated certificate": {web(func(w *config.WebConfig) { w.TLS.Enabled = true }), "https://localhost:3080"},
		"own certificate":       {web(func(w *config.WebConfig) { w.TLS.Cert, w.TLS.Key = "/c.pem", "/k.pem" }), "https://localhost:3080"},
		// A configured host name is the one the certificate was made for.
		"configured host name": {web(func(w *config.WebConfig) { w.TLS.Enabled, w.TLS.Hostname = true, "legato.lan" }), "https://legato.lan:3080"},
	}
	for name, tc := range cases {
		if got := localWebURL(tc.cfg); got != tc.want {
			t.Errorf("%s: localWebURL = %q, want %q", name, got, tc.want)
		}
	}
}
