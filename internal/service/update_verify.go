package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// verifyLegatoBinary checks a downloaded Legato before it replaces the
// running one: it must run, and say it is the release it came from.
// `legato version` reads no configuration, so this touches nothing.
func verifyLegatoBinary(want string) func(ctx context.Context, binary string) error {
	return func(ctx context.Context, binary string) error {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, binary, "version", "--json").Output()
		if err != nil {
			return fmt.Errorf("does not run: %w", err)
		}
		var envelope struct {
			Data struct {
				Version string `json:"version"`
			} `json:"data"`
		}
		if err := json.Unmarshal(out, &envelope); err != nil {
			return fmt.Errorf("gave no version: %q", strings.TrimSpace(string(out)))
		}
		if envelope.Data.Version != want {
			return fmt.Errorf("says it is %s, not %s", envelope.Data.Version, want)
		}
		return nil
	}
}
