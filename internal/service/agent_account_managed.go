package service

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func validateNamedAccountManagedSettings(dir string) error {
	if err := inspectNamedAccountSettings(dir, "managed-settings.json"); err != nil {
		return err
	}
	parts := filepath.Join(dir, "managed-settings.d")
	info, err := os.Lstat(parts)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("named Claude subscription: managed settings directory cannot be inspected safely; repair the policy directory before launching")
	}
	files, err := os.ReadDir(parts)
	if err != nil {
		return errors.New("named Claude subscription: managed settings cannot be read; repair the policy directory before launching")
	}
	for _, file := range files {
		if strings.HasPrefix(file.Name(), ".") || !strings.HasSuffix(file.Name(), ".json") {
			continue
		}
		if err := inspectNamedAccountSettings(parts, file.Name()); err != nil {
			return err
		}
	}
	return nil
}
