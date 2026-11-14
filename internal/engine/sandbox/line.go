package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// ArgsFileLine writes a bwrap-like backend's options to dir/bwrap.args,
// NUL-separated, and the terminal filter (TerminalFilter) to
// dir/seccomp.bpf, and returns the short line that reads them on fds 3 and
// 4: a launch command is typed into a terminal, which cuts a line at 4096
// bytes before the shell reads it.
func ArgsFileLine(binary string, options []string, dir string) (string, error) {
	filter, err := TerminalFilter(runtime.GOARCH)
	if err != nil {
		return "", err
	}
	file, seccomp := filepath.Join(dir, "bwrap.args"), filepath.Join(dir, "seccomp.bpf")
	options = append(append([]string{}, options...), "--seccomp", "4")
	var data []byte
	for _, option := range options {
		if strings.ContainsRune(option, 0) {
			return "", fmt.Errorf("sandbox option %q holds a NUL byte", option)
		}
		data = append(append(data, option...), 0)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(file, data, 0o600); err != nil {
		return "", err
	}
	if err := os.WriteFile(seccomp, filter, 0o600); err != nil {
		return "", err
	}
	return ShellWord(binary) + " --args 3 3<" + ShellWord(file) + " 4<" + ShellWord(seccomp) + " --", nil
}

var plainWord = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

// ShellWord quotes a word for the shell only when it needs it.
func ShellWord(s string) string {
	if plainWord.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
