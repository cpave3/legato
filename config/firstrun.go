package config

import (
	"errors"
	"os"
	"path/filepath"
)

// firstConfig is the config.yaml a first run writes: no settings, since
// Legato needs none of its file-only ones to start, and a pointer to where
// the rest are changed.
const firstConfig = `# Legato's configuration.
#
# Most settings are kept in Legato's database and changed in the web UI
# (Settings), or listed with: legato config show --sources
# A setting written here pins it: the file wins, and Settings shows it locked.
#
# This file holds what Legato needs before its database opens, how it is
# reached, and what unseals its secrets. Uncomment what you need:
#
# db:
#   path: ~/.local/share/legato/legato.db
# data_dir: ~/.local/share/legato
# web:
#   enabled: true      # start the web server with the TUI
#   port: "3080"
#   tls:
#     enabled: true
#   public_url: https://legato.example.ts.net
# secrets:
#   master_key: auto
#
# Every setting: https://cpave3.github.io/legato/reference/configuration/
`

// WriteFirstConfig writes the first run's config.yaml at path when there is
// none, and reports whether it did. A file already there is never touched.
func WriteFirstConfig(path string) (bool, error) {
	if _, err := os.Stat(path); err == nil || !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err := f.WriteString(firstConfig); err != nil {
		f.Close()
		return false, err
	}
	return true, f.Close()
}
