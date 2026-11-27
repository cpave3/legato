package main

import "runtime"

func claudeManagedSettingsDir() string {
	if runtime.GOOS == "darwin" {
		return "/Library/Application Support/ClaudeCode"
	}
	return "/etc/claude-code"
}
