package main

import (
	"testing"

	"github.com/cpave3/legato/config"
)

func TestServeListensOnWebPortUnlessToldOtherwise(t *testing.T) {
	tests := []struct {
		name, flag, webPort, want string
	}{
		{"web.port", "", "3391", "3391"},
		{"--port wins", "4000", "3391", "4000"},
		{"neither", "", "", "3080"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{Web: config.WebConfig{Port: tc.webPort}}
			if got := servePort(tc.flag, cfg); got != tc.want {
				t.Errorf("servePort(%q) with web.port %q = %q, want %q", tc.flag, tc.webPort, got, tc.want)
			}
			// Links the server hands out (to agents, in notifications) read
			// web.port, so it must name the port the server listens on.
			if cfg.Web.Port != tc.want {
				t.Errorf("web.port is %q after servePort, want %q", cfg.Web.Port, tc.want)
			}
		})
	}
}
