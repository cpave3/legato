package service

import (
	"context"
	"strings"

	"github.com/cpave3/legato/config"
	"gopkg.in/yaml.v3"
)

// Export writes the settings set in the database as YAML shaped like
// config.yaml: single values by key, and list entries under their list.
// With pinned, the settings and entries config.yaml pins are included too.
// Secrets are written as names: a secret setting the database keeps is a
// name already, and one the file holds is written only when it is a
// reference (an ${ENV} or a literal never leaves).
func (s *SettingsService) Export(ctx context.Context, pinned bool) ([]byte, error) {
	values, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	lists, err := s.Lists(ctx)
	if err != nil {
		return nil, err
	}
	// The listing hides a secret the file pins; the export reads the file's
	// own value, and keeps it only when it is a name.
	file, err := s.load()
	if err != nil {
		return nil, err
	}
	return config.EditBytes(nil, func(doc *yaml.Node) error {
		header := "Legato settings exported with legato config export"
		if pinned {
			header += " --pinned"
		}
		doc.Content[0].HeadComment = header + ".\nSecrets are names of secrets (legato://, op://), never values."
		for _, v := range values {
			value := v.Value
			switch {
			case v.Source == SettingFromDatabase:
			case v.Source == SettingFromFile && pinned:
				if v.Secret {
					value, _ = config.SettingValue(file, v.Key)
					if str, ok := value.(string); !ok || !isSecretRef(str) {
						continue
					}
				}
			default:
				continue
			}
			if err := config.SetKey(doc, v.Key, value); err != nil {
				return err
			}
		}
		for _, l := range lists {
			var entries []any
			byName := map[string]any{}
			for _, e := range l.Entries {
				if e.Source == SettingFromFile && !pinned {
					continue
				}
				value := e.Value
				if m, ok := value.(map[string]any); ok && l.NameField != "" {
					copied := map[string]any{l.NameField: e.Name}
					for k, v := range m {
						if k != l.NameField {
							copied[k] = v
						}
					}
					if secret, ok := copied["secret"].(string); ok && !isSecretRef(secret) {
						continue
					}
					value = copied
				}
				entries = append(entries, value)
				byName[e.Name] = e.Value
			}
			if len(entries) == 0 {
				continue
			}
			var listValue any = entries
			if l.NameField == "" {
				listValue = byName
			}
			if err := config.SetKey(doc, l.Key, listValue); err != nil {
				return err
			}
		}
		return nil
	})
}

func isSecretRef(s string) bool {
	return strings.HasPrefix(s, "legato://") || strings.HasPrefix(s, "op://")
}
