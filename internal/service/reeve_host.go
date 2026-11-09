package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
)

// Kinds of host operation in a change set.
const (
	CallHostShell     = "host_shell"
	CallEditFile      = "edit_file"
	CallManageService = "manage_service"
)

// ErrHostOperationsOff: host operations are turned off (reeve.host_operations).
var ErrHostOperationsOff = errors.New("host operations are off for the Reeve (Settings › Reeve)")

var unitName = regexp.MustCompile(`^[A-Za-z0-9@._:-]+$`)

var serviceActions = map[string]bool{"start": true, "stop": true, "restart": true, "status": true, "logs": true}

// HostCommand is how a host operation is run: its argv, with no shell but
// the one a host_shell call names.
type HostCommand struct {
	Argv   []string
	AsRoot bool
}

// HostExecutor runs a host command as Legato's user, in its home folder,
// with a small fixed environment and no way to gain privileges (sudo
// fails), unless as root; its exit code and full output.
type HostExecutor func(ctx context.Context, cmd HostCommand, timeout time.Duration) (int, []byte, error)

// isHostCall reports whether a call kind is a host operation.
func isHostCall(kind string) bool {
	return kind == CallHostShell || kind == CallEditFile || kind == CallManageService
}

// describeHost fills a host operation's class, description and level: all
// risky, at least an approval, and a call as root always signed and
// verified.
func (c *ChangeSetService) describeHost(cfg *config.Config, enrolled bool, i int, p *ProposedCall) error {
	if !cfg.Reeve.HostOperationsOn() {
		return ErrHostOperationsOff
	}
	p.Class = "risky"
	switch p.Kind {
	case CallHostShell:
		if strings.TrimSpace(p.Command) == "" {
			return fmt.Errorf("call %d: a shell call needs a command", i+1)
		}
		p.Description = "Run on the host: " + p.Command
		if p.AsRoot {
			p.Description = "Run on the host as root: " + p.Command
		}
	case CallEditFile:
		if !filepath.IsAbs(p.File) {
			return fmt.Errorf("call %d: edit_file needs an absolute path", i+1)
		}
		if p.AsRoot {
			return fmt.Errorf("call %d: edit_file runs as Legato's user; edit a root file with host_shell as root", i+1)
		}
		old, err := os.ReadFile(p.File)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("call %d: %w", i+1, err)
		}
		p.Diff = lineDiff(string(old), p.Content)
		p.Description = "Write the file " + p.File
	case CallManageService:
		if !unitName.MatchString(p.Unit) || !serviceActions[p.Action] {
			return fmt.Errorf("call %d: manage_service needs a unit name and one of start, stop, restart, status, logs", i+1)
		}
		scope := "user"
		if p.System {
			scope = "system"
		}
		p.Description = fmt.Sprintf("%s the %s service %s", strings.ToUpper(p.Action[:1])+p.Action[1:], scope, p.Unit)
		if p.AsRoot {
			p.Description += " as root"
		}
	}
	p.Level = ReeveLevelFor(cfg.Reeve, "risky", p.Kind+" "+p.Command+p.File+p.Unit, enrolled)
	if p.AsRoot {
		p.Level = LevelVerified
	}
	return nil
}

// hostCommand is the argv a host call runs.
func hostCommand(p ProposedCall) HostCommand {
	switch p.Kind {
	case CallManageService:
		if p.Action == "logs" {
			argv := []string{"journalctl", "--no-pager", "-n", "200", "-u", p.Unit}
			if !p.System {
				argv = append(argv[:1], append([]string{"--user"}, argv[1:]...)...)
			}
			return HostCommand{Argv: argv, AsRoot: p.AsRoot}
		}
		argv := []string{"systemctl"}
		if !p.System {
			argv = append(argv, "--user")
		}
		argv = append(argv, p.Action, p.Unit)
		if p.Action == "status" {
			argv = append(argv, "--no-pager")
		}
		return HostCommand{Argv: argv, AsRoot: p.AsRoot}
	}
	return HostCommand{Argv: []string{"sh", "-c", p.Command}, AsRoot: p.AsRoot}
}

// runHost runs one host call of a set: its result, with the output cut at
// the cap and the whole of it kept in a file the Reeve can read.
func (c *ChangeSetService) runHost(ctx context.Context, cfg *config.Config, set store.ReeveChangeSet, n int, p ProposedCall) (CallResult, string, []byte) {
	res := CallResult{Call: n}
	var exit int
	var output []byte
	var err error
	switch p.Kind {
	case CallEditFile:
		var backup string
		backup, err = writeFileWithBackup(p.File, []byte(p.Content))
		output = []byte("written; the old file is " + backup)
		if backup == "" {
			output = []byte("written (a new file)")
		}
	default:
		if c.opts.HostExec == nil {
			err = errors.New("host commands cannot be run here")
			break
		}
		exit, output, err = c.opts.HostExec(ctx, hostCommand(p), cfg.Reeve.HostCommandTimeout())
	}
	res.Status = exit
	text := string(output)
	if limit := cfg.Reeve.HostOutputLimit(); len(output) > limit && c.opts.OutputDir != "" {
		path := filepath.Join(c.opts.OutputDir, fmt.Sprintf("%s-%d.log", set.ID, n))
		if os.MkdirAll(c.opts.OutputDir, 0o700) == nil && os.WriteFile(path, output, 0o600) == nil {
			text = string(output[:limit]) + fmt.Sprintf("\n… cut at %d bytes; the whole output is in %s", limit, path)
		}
	}
	switch {
	case err != nil:
		res.State, res.Detail = "failed", err.Error()
	case exit != 0:
		res.State, res.Detail = "failed", clip(strings.TrimSpace(text), 2000)
	default:
		res.State, res.Detail = "ran", clip(strings.TrimSpace(text), 2000)
	}
	return res, text, output
}

// auditHost appends a host action to the audit chain: what ran, as root or
// not, who approved it with which device and verification, the exit code
// and a hash of the output.
func (c *ChangeSetService) auditHost(ctx context.Context, set store.ReeveChangeSet, decision *ApprovalDecision, p ProposedCall, res CallResult, output []byte) {
	sum := sha256.Sum256(output)
	resource := p.Command
	switch p.Kind {
	case CallEditFile:
		resource = p.File
	case CallManageService:
		resource = p.Action + " " + p.Unit
	}
	detail, _ := json.Marshal(map[string]any{"root": p.AsRoot, "exit": res.Status, "state": res.State, "output_sha256": hex.EncodeToString(sum[:]), "change_set": set.ID})
	entry := store.CredentialAuditEntry{Event: "host", TaskID: set.TaskID, Operation: p.Kind, Resource: resource, ApprovalID: set.ApprovalID, Detail: string(detail)}
	if decision != nil {
		entry.DecidedBy = decidedBy(*decision)
		entry.Verification = decision.Verification
	}
	if _, err := c.opts.Store.AppendCredentialAudit(ctx, entry); err != nil {
		fmt.Fprintf(os.Stderr, "reeve: audit of %s: %v\n", set.ID, err)
	}
}

// writeFileWithBackup writes path atomically, keeping the old file beside
// it as <path>.<UTC time>.bak; the backup is "" for a new file.
func writeFileWithBackup(path string, content []byte) (string, error) {
	backup := ""
	if old, err := os.ReadFile(path); err == nil {
		backup = config.BackupPath(path, time.Now())
		if err := os.WriteFile(backup, old, 0o600); err != nil {
			return "", err
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	tmp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".legato-tmp")
	if err := os.WriteFile(tmp, content, mode); err != nil {
		return backup, err
	}
	return backup, os.Rename(tmp, path)
}
