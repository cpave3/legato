package opendeck

import (
	"testing"
)

func TestParseArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
		port    string
		uuid    string
		regEv   string
	}{
		{
			name:    "valid args",
			args:    []string{"-port", "57116", "-pluginUUID", "abc-123", "-registerEvent", "registerPlugin"},
			wantErr: false,
			port:    "57116",
			uuid:    "abc-123",
			regEv:   "registerPlugin",
		},
		{
			name:    "missing port",
			args:    []string{"-pluginUUID", "abc-123", "-registerEvent", "registerPlugin"},
			wantErr: true,
		},
		{
			name:    "missing uuid",
			args:    []string{"-port", "57116", "-registerEvent", "registerPlugin"},
			wantErr: true,
		},
		{
			name:    "missing register event",
			args:    []string{"-port", "57116", "-pluginUUID", "abc-123"},
			wantErr: true,
		},
		{
			name:    "empty args",
			args:    []string{},
			wantErr: true,
		},
		{
			name:    "extra args ignored",
			args:    []string{"-port", "57116", "-pluginUUID", "abc-123", "-registerEvent", "registerPlugin", "-extra", "value"},
			wantErr: false,
			port:    "57116",
			uuid:    "abc-123",
			regEv:   "registerPlugin",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := ParseArgs(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if a.Port != tt.port {
				t.Errorf("port: got %q, want %q", a.Port, tt.port)
			}
			if a.PluginUUID != tt.uuid {
				t.Errorf("uuid: got %q, want %q", a.PluginUUID, tt.uuid)
			}
			if a.RegisterEv != tt.regEv {
				t.Errorf("register event: got %q, want %q", a.RegisterEv, tt.regEv)
			}
		})
	}
}

func TestMaxTiles(t *testing.T) {
	if MaxTiles != 15 {
		t.Errorf("MaxTiles = %d, want 15", MaxTiles)
	}
}

func TestActionUUIDs(t *testing.T) {
	tests := []struct {
		got, want string
	}{
		{ActionHome, "com.cpave3.legato.home"},
		{ActionAgentTile, "com.cpave3.legato.agent-tile"},
		{ActionAttnTile, "com.cpave3.legato.attention-tile"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("action UUID: got %q, want %q", tt.got, tt.want)
		}
	}
}
