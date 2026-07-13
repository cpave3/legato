package hooks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const yggdrasilHook = `'command -v legato >/dev/null 2>&1 || exit 0; [ -z "$LEGATO_TASK_ID" ] && exit 0; legato task worktree set "$LEGATO_TASK_ID" --path "$YG_WORKTREE" --primary-dir "$YG_PRIMARY" --branch "$YG_BRANCH" --base-branch "$YG_BASE"'`

type YggdrasilAdapter struct{}

func NewYggdrasilAdapter(_ string) *YggdrasilAdapter              { return &YggdrasilAdapter{} }
func (a *YggdrasilAdapter) Name() string                          { return "yggdrasil" }
func (a *YggdrasilAdapter) EnvVars(_, _ string) map[string]string { return nil }

func (a *YggdrasilAdapter) InstallHooks(projectDir string) error {
	return rewriteYggdrasil(projectDir, true)
}

func (a *YggdrasilAdapter) UninstallHooks(projectDir string) error {
	return rewriteYggdrasil(projectDir, false)
}

func rewriteYggdrasil(projectDir string, install bool) error {
	path := filepath.Join(projectDir, ".yggdrasil.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) && !install {
			return nil
		}
		if os.IsNotExist(err) {
			return fmt.Errorf("%s not found", path)
		} else {
			return fmt.Errorf("reading %s: %w", path, err)
		}
	}
	var parsed map[string]any
	if err := toml.Unmarshal(data, &parsed); err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}
	text := string(data)
	start, end := postCreateRange(text)
	if start < 0 {
		if !install {
			return nil
		}
		if text != "" && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		text += "[hooks]\npost_create = [\n  " + yggdrasilHook + ",\n]\n"
	} else {
		value := text[start:end]
		items := tomlArrayItems(value)
		kept := make([]string, 0, len(items)+1)
		for _, item := range items {
			if !isLegatoWorktreeHook(item) {
				kept = append(kept, strings.TrimSpace(item))
			}
		}
		if install {
			kept = append(kept, yggdrasilHook)
		}
		var b strings.Builder
		b.WriteString("[\n")
		for _, item := range kept {
			if item != "" {
				b.WriteString("  ")
				b.WriteString(item)
				b.WriteString(",\n")
			}
		}
		b.WriteString("]")
		text = text[:start] + b.String() + text[end:]
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

func postCreateRange(text string) (int, int) {
	section := ""
	offset := 0
	for _, line := range strings.SplitAfter(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			section = trimmed
		}
		if section == "[hooks]" && strings.HasPrefix(trimmed, "post_create") {
			eq := strings.Index(line, "=")
			if eq < 0 {
				return -1, -1
			}
			start := offset + eq + 1
			for start < len(text) && (text[start] == ' ' || text[start] == '\t') {
				start++
			}
			open := strings.Index(text[start:], "[")
			if open < 0 {
				return -1, -1
			}
			start += open
			quote := byte(0)
			for i := start; i < len(text); i++ {
				c := text[i]
				if quote != 0 {
					if c == quote {
						quote = 0
					}
					continue
				}
				if c == '\'' || c == '"' {
					quote = c
					continue
				}
				if c == ']' {
					return start, i + 1
				}
			}
		}
		offset += len(line)
	}
	return -1, -1
}

func tomlArrayItems(value string) []string {
	inner := strings.TrimSpace(value)
	if len(inner) >= 2 {
		inner = inner[1 : len(inner)-1]
	}
	var items []string
	start := 0
	quote := byte(0)
	for i := 0; i < len(inner); i++ {
		c := inner[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
		} else if c == ',' {
			items = append(items, inner[start:i])
			start = i + 1
		}
	}
	if strings.TrimSpace(inner[start:]) != "" {
		items = append(items, inner[start:])
	}
	return items
}

func isLegatoWorktreeHook(item string) bool {
	return strings.Contains(item, "legato task worktree set")
}
