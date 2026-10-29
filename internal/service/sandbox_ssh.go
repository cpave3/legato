package service

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// SSHConfigCopy is a copy of one file of the system ssh configuration, owned
// by the person, and the real path of the file it stands in for.
type SSHConfigCopy struct {
	Copy, Original string
}

// writeSandboxSSHConfig copies the system ssh configuration under etcSSH to
// dir: ssh_config and every file it includes, as ssh would read them. In a
// sandbox's user namespace the originals belong to nobody, and ssh refuses
// them. The copy of ssh_config first names dir/known_hosts as the known
// hosts file, where the sandbox mounts the one Legato's SSH agent uses.
func writeSandboxSSHConfig(etcSSH, dir string) ([]SSHConfigCopy, error) {
	main := filepath.Join(etcSSH, "ssh_config")
	if _, err := os.Stat(main); err != nil {
		return nil, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	var copies []SSHConfigCopy
	seen := map[string]bool{}
	var walk func(path string, depth int) error
	walk = func(path string, depth int) error {
		real, err := filepath.EvalSymlinks(path)
		if err != nil || seen[real] || depth > 16 {
			return nil
		}
		seen[real] = true
		data, err := os.ReadFile(real)
		if err != nil {
			return nil
		}
		copyPath := filepath.Join(dir, strconv.Itoa(len(copies))+"-"+filepath.Base(real))
		content := data
		if len(copies) == 0 {
			header := fmt.Sprintf("# Written by Legato for an agent's sandbox.\nUserKnownHostsFile %s\n\n", filepath.Join(dir, "known_hosts"))
			content = append([]byte(header), data...)
		}
		if err := os.WriteFile(copyPath, content, 0o600); err != nil {
			return err
		}
		copies = append(copies, SSHConfigCopy{Copy: copyPath, Original: real})
		for _, pattern := range sshIncludes(data) {
			if !filepath.IsAbs(pattern) {
				pattern = filepath.Join(etcSSH, pattern)
			}
			matches, _ := filepath.Glob(pattern)
			for _, match := range matches {
				if err := walk(match, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(main, 0); err != nil {
		return nil, err
	}
	placeholder := filepath.Join(dir, "known_hosts")
	if _, err := os.Stat(placeholder); os.IsNotExist(err) {
		if err := os.WriteFile(placeholder, nil, 0o600); err != nil {
			return nil, err
		}
	}
	return copies, nil
}

// sshIncludes lists the patterns of a configuration's Include lines. A
// pattern that starts with ~ names the person's own files, which the
// sandbox does not have.
func sshIncludes(data []byte) []string {
	var patterns []string
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		fields := strings.Fields(strings.Replace(scanner.Text(), "=", " ", 1))
		if len(fields) < 2 || !strings.EqualFold(fields[0], "include") {
			continue
		}
		for _, f := range fields[1:] {
			f = strings.Trim(f, `"`)
			if f != "" && !strings.HasPrefix(f, "~") {
				patterns = append(patterns, f)
			}
		}
	}
	return patterns
}
