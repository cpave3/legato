package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadAgentsConfig(t *testing.T, yaml string) (*Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LEGATO_CONFIG", path)
	return Load()
}

func TestAgentsReviewsDefaultOnAndCanBeTurnedOff(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		t.Setenv("LEGATO_CONFIG", filepath.Join(t.TempDir(), "missing.yaml"))
		cfg, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if !cfg.Agents.Reviews {
			t.Fatal("reviews should default on")
		}
	})

	t.Run("an unrelated agents setting keeps them on", func(t *testing.T) {
		cfg, err := loadAgentsConfig(t, "agents:\n  peer_awareness: false\n")
		if err != nil {
			t.Fatal(err)
		}
		if !cfg.Agents.Reviews {
			t.Fatal("reviews should stay on")
		}
	})

	t.Run("off", func(t *testing.T) {
		cfg, err := loadAgentsConfig(t, "agents:\n  reviews: false\n")
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Agents.Reviews {
			t.Fatal("reviews should honor explicit false")
		}
	})
}

func TestAgentsGuidanceIsKeyedByRepository(t *testing.T) {
	t.Run("entries load with the repository normalised", func(t *testing.T) {
		cfg, err := loadAgentsConfig(t, `agents:
  guidance:
    - repo: Acme/Web-App
      text: |
        Run the prove-it skill when the work is done.
    - repo: git@gitlab.example.com:team/service.git
      text: Keep migrations reversible.
`)
		if err != nil {
			t.Fatal(err)
		}
		got := cfg.Agents.Guidance
		if len(got) != 2 {
			t.Fatalf("guidance = %#v", got)
		}
		if got[0].Repo != "github.com/acme/web-app" || got[0].Text != "Run the prove-it skill when the work is done." {
			t.Fatalf("first entry = %#v", got[0])
		}
		if got[1].Repo != "gitlab.example.com/team/service" || got[1].Text != "Keep migrations reversible." {
			t.Fatalf("second entry = %#v", got[1])
		}
	})

	for name, tc := range map[string]struct{ yaml, want string }{
		"a repository that is not one": {
			yaml: "agents:\n  guidance:\n    - repo: just-a-name\n      text: hi\n",
			want: `agents.guidance[0].repo "just-a-name"`,
		},
		"no text": {
			yaml: "agents:\n  guidance:\n    - repo: acme/web-app\n      text: \"  \"\n",
			want: "agents.guidance[0].text",
		},
		"the same repository twice": {
			yaml: "agents:\n  guidance:\n    - repo: acme/web-app\n      text: a\n    - repo: github.com/Acme/web-app\n      text: b\n",
			want: "agents.guidance[1]: github.com/acme/web-app already has guidance",
		},
	} {
		t.Run(name+" is refused", func(t *testing.T) {
			_, err := loadAgentsConfig(t, tc.yaml)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestNormalizeRepository(t *testing.T) {
	for raw, want := range map[string]string{
		"cpave3/legato":                        "github.com/cpave3/legato",
		"https://github.com/CPave3/Legato.git": "github.com/cpave3/legato",
		"git@github.com:cpave3/legato.git":     "github.com/cpave3/legato",
		"ssh://git@gitlab.example.com/a/b":     "gitlab.example.com/a/b",
		"bitbucket.org/team/repo/":             "bitbucket.org/team/repo",
	} {
		if got, ok := NormalizeRepository(raw); !ok || got != want {
			t.Errorf("NormalizeRepository(%q) = %q, %v; want %q", raw, got, ok, want)
		}
	}
	for _, raw := range []string{"", "legato", "a/b/c/d", "https://github.com/", "owner/na me"} {
		if got, ok := NormalizeRepository(raw); ok {
			t.Errorf("NormalizeRepository(%q) = %q, want refused", raw, got)
		}
	}
}
