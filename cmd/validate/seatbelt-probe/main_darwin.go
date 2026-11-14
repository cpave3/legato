//go:build darwin

// Command seatbelt-probe answers, on macOS, the questions that Legato's
// Seatbelt sandbox depends on (LEG-160, LEG-168), and prints the answers as
// Markdown. Each probe runs commands under /usr/bin/sandbox-exec with a small
// profile that isolates one question.
//
// It changes nothing outside its own temporary folder. The keychain it reads
// is one it makes there and deletes; it never reads a secret of the
// person's (for the agent tools' logins it reports only whether an entry
// exists).
//
//	go run ./cmd/validate/seatbelt-probe > seatbelt-probe.md
package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

const sandboxExec = "/usr/bin/sandbox-exec"

// allowDefault starts a profile that allows everything, so a probe can deny
// just what it asks about.
const allowDefault = "(version 1)\n(allow default)\n"

const (
	secretMarker = "probe-secret-7731"
	argMarker    = "probe-arg-7731"
)

type row struct{ variant, command, outcome string }

type section struct {
	title, question string
	rows            []row
	notes           []string
}

var sections []*section

func newSection(title, question string) *section {
	s := &section{title: title, question: question}
	sections = append(sections, s)
	return s
}

func (s *section) add(variant, command, outcome string) {
	s.rows = append(s.rows, row{variant, command, outcome})
}

func (s *section) note(format string, args ...any) {
	s.notes = append(s.notes, fmt.Sprintf(format, args...))
}

func main() {
	if len(os.Args) > 2 && os.Args[1] == "helper" {
		os.Exit(helper(os.Args[2], os.Args[3:]))
	}
	if _, err := os.Stat(sandboxExec); err != nil {
		fmt.Fprintln(os.Stderr, "no", sandboxExec, err)
		os.Exit(1)
	}
	self, err := os.Executable()
	if err == nil {
		self, err = filepath.EvalSymlinks(self)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot find this program:", err)
		os.Exit(1)
	}
	tmp, err := os.MkdirTemp("", "lgprobe")
	if err == nil {
		tmp, err = filepath.EvalSymlinks(tmp)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmp)

	probeSyntax(tmp)
	probeTLS(self)
	probeKeychain(tmp)
	probeHomeKeychain(tmp)
	probeToolLogins()
	probeProcArgs(self)
	probeSignals()
	probeFirmlink(tmp)
	probeLinks(self, tmp)
	probeTIOCSTI(self)
	probeShells()
	probeUnixSockets(self, tmp)
	probeNested()
	report()
}

// run runs a command and returns its combined output and exit code (-1: it
// did not start, -2: it timed out).
func run(timeout time.Duration, env []string, name string, args ...string) (string, int) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	if env != nil {
		cmd.Env = env
	}
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if ctx.Err() != nil {
		return text, -2
	}
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return text, exit.ExitCode()
		}
		return err.Error(), -1
	}
	return text, 0
}

// sbx runs a command in a sandbox with profile; params are KEY=VALUE.
func sbx(profile string, params []string, command ...string) (string, int) {
	args := []string{"-p", profile}
	for _, p := range params {
		args = append(args, "-D"+p)
	}
	args = append(append(args, "--"), command...)
	return run(90*time.Second, nil, sandboxExec, args...)
}

// short is the first lines of output, for a table cell.
func short(out string, code int) string {
	out = strings.ReplaceAll(out, "|", "\\|")
	lines := strings.Split(out, "\n")
	if len(lines) > 3 {
		lines = append(lines[:3], "…")
	}
	text := strings.Join(lines, " ⏎ ")
	if len(text) > 300 {
		text = text[:300] + "…"
	}
	return fmt.Sprintf("exit %d: %s", code, text)
}

func lookPath(name string) string {
	p, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return p
}

// probeSyntax asks which profile constructs this macOS accepts.
func probeSyntax(tmp string) {
	s := newSection("Profile syntax", "Which SBPL constructs does this macOS accept? (`sandbox-exec -p '(version 1)(allow default) <rule>' /usr/bin/true`)")
	rules := []string{
		`(deny file-read-data (literal "/nonexistent-lgprobe") (with message "legato-probe"))`,
		`(allow file-read* (subpath (param "P0")))`,
		`(deny file-ioctl (ioctl-command TIOCSTI))`,
		fmt.Sprintf(`(deny file-ioctl (ioctl-command %d))`, uint32(unix.TIOCSTI)),
		`(deny mach-lookup (xpc-service-name-prefix ""))`,
		`(deny mach-lookup (global-name "com.apple.SecurityServer") (global-name "com.apple.securityd.xpc"))`,
		`(deny job-creation)`,
		`(deny lsopen)`,
		`(deny appleevent-send)`,
		`(deny system-fcntl (fcntl-command 80 110))`,
		`(deny file-write-setugid)`,
		`(deny file-link (subpath "/nonexistent-lgprobe"))`,
		`(deny file-clone (subpath "/nonexistent-lgprobe"))`,
		`(deny file-write-unlink (require-all (vnode-type DIRECTORY) (literal "/nonexistent-lgprobe")))`,
		`(allow file-read-metadata (path-ancestors "/System/Volumes/Data/private"))`,
		`(allow file-read* file-write* (require-all (regex #"^/dev/ttys[0-9]+") (extension "com.apple.sandbox.pty")))`,
		`(allow pseudo-tty)`,
		`(allow user-preference-read (preference-domain "kCFPreferencesAnyApplication"))`,
		`(allow system-socket (socket-domain AF_UNIX))`,
		`(allow network-outbound (remote unix-socket (subpath "/nonexistent-lgprobe")))`,
		`(allow network-outbound (remote unix-socket (path-literal "/nonexistent-lgprobe")))`,
		`(allow network-bind (local unix-socket (subpath "/nonexistent-lgprobe")))`,
		`(allow network-outbound (remote ip "localhost:*"))`,
		`(allow network-inbound (local ip "*:*"))`,
		`(deny sysctl-read (sysctl-name-prefix "kern.procargs"))`,
		`(deny sysctl-read (sysctl-name "kern.proc.all"))`,
		`(allow process-info* (target same-sandbox))`,
		`(allow signal (target same-sandbox))`,
		`(allow mach-priv-task-port (target same-sandbox))`,
		`(deny process-exec (literal "/usr/bin/sudo"))`,
		`(allow ipc-posix-shm)`,
		`(allow ipc-posix-sem)`,
		`(allow iokit-open (iokit-registry-entry-class "RootDomainUserClient"))`,
	}
	for _, rule := range rules {
		out, code := sbx(allowDefault+rule, []string{"P0=" + tmp}, "/usr/bin/true")
		outcome := "accepted"
		if code != 0 {
			outcome = "refused: " + short(out, code)
		}
		s.add("", "`"+rule+"`", outcome)
	}
}

func probeTLS(self string) {
	s := newSection("TLS without securityd", "Does TLS work when `com.apple.SecurityServer` and `com.apple.securityd.xpc` are denied? (The Keychain is read through them.)")
	type tool struct {
		name string
		cmd  []string
	}
	tools := []tool{{"Go (net/http, platform verifier)", []string{self, "helper", "tls", "https://github.com"}},
		{"curl", []string{"/usr/bin/curl", "-sS", "-o", "/dev/null", "-w", "%{http_code}", "https://github.com"}}}
	if git := lookPath("git"); git != "" {
		tools = append(tools, tool{"git", []string{git, "-c", "credential.helper=", "ls-remote", "https://github.com/cpave3/legato", "HEAD"}})
	}
	if node := lookPath("node"); node != "" {
		tools = append(tools, tool{"node fetch", []string{node, "-e", `fetch("https://github.com").then(r=>console.log(r.status)).catch(e=>{console.error(String(e.cause||e));process.exit(1)})`}})
	}
	if cargo := lookPath("cargo"); cargo != "" {
		tools = append(tools, tool{"cargo (libcurl)", []string{cargo, "search", "--limit", "1", "serde"}})
	}
	variants := []struct{ name, rules string }{
		{"allow default", ""},
		{"deny SecurityServer + securityd.xpc", `(deny mach-lookup (global-name "com.apple.SecurityServer") (global-name "com.apple.securityd.xpc"))`},
		{"… and trustd.agent", `(deny mach-lookup (global-name "com.apple.SecurityServer") (global-name "com.apple.securityd.xpc") (global-name "com.apple.trustd.agent") (global-name "com.apple.trustd"))`},
	}
	for _, v := range variants {
		for _, t := range tools {
			out, code := sbx(allowDefault+v.rules, nil, t.cmd...)
			s.add(v.name, t.name, short(out, code))
		}
	}
}

// probeKeychain makes a keychain of its own, saves an item the way gh
// (go-keyring) does, through /usr/bin/security, and asks who can read it.
func probeKeychain(tmp string) {
	s := newSection("Keychain items", "Can a sandboxed process read a Keychain item that `/usr/bin/security` saved (as `gh` saves its token), and what closes it?")
	sec := "/usr/bin/security"
	before, _ := run(10*time.Second, nil, sec, "list-keychains", "-d", "user")
	kc := filepath.Join(tmp, "lgprobe.keychain")
	steps := [][]string{
		{"create-keychain", "-p", "lgprobe", kc},
		{"unlock-keychain", "-p", "lgprobe", kc},
		{"set-keychain-settings", kc},
		{"add-generic-password", "-s", "legato-probe", "-a", "probe", "-w", secretMarker, kc},
	}
	for _, step := range steps {
		if out, code := run(20*time.Second, nil, sec, step...); code != 0 {
			s.note("setup `security %s` failed: %s", step[0], short(out, code))
			cleanupKeychain(s, kc, before)
			return
		}
	}
	defer cleanupKeychain(s, kc, before)
	read := []string{sec, "find-generic-password", "-s", "legato-probe", "-w", kc}
	out, code := run(20*time.Second, nil, read[0], read[1:]...)
	s.add("outside the sandbox", "security find-generic-password -w", secretOutcome(out, code))
	params := []string{"KC=" + filepath.Dir(kc)}
	variants := []struct{ name, rules string }{
		{"allow default", ""},
		{"deny the keychain's folder (files only)", `(deny file-read* file-write* (subpath (param "KC")))`},
		{"deny SecurityServer + securityd.xpc", `(deny mach-lookup (global-name "com.apple.SecurityServer") (global-name "com.apple.securityd.xpc"))`},
		{"deny both", `(deny file-read* file-write* (subpath (param "KC")))(deny mach-lookup (global-name "com.apple.SecurityServer") (global-name "com.apple.securityd.xpc"))`},
	}
	for _, v := range variants {
		out, code := sbx(allowDefault+v.rules, params, read...)
		s.add(v.name, "security find-generic-password -w", secretOutcome(out, code))
	}
}

func cleanupKeychain(s *section, kc, before string) {
	sec := "/usr/bin/security"
	_, _ = run(20*time.Second, nil, sec, "delete-keychain", kc)
	after, _ := run(10*time.Second, nil, sec, "list-keychains", "-d", "user")
	if after != before {
		var list []string
		for _, line := range strings.Split(before, "\n") {
			if l := strings.Trim(strings.TrimSpace(line), `"`); l != "" {
				list = append(list, l)
			}
		}
		_, _ = run(10*time.Second, nil, sec, append([]string{"list-keychains", "-d", "user", "-s"}, list...)...)
		s.note("the user keychain search list changed and was put back")
	}
}

func secretOutcome(out string, code int) string {
	if strings.Contains(out, secretMarker) {
		return "**read the secret**"
	}
	return "did not read it: " + short(out, code)
}

func probeHomeKeychain(tmp string) {
	s := newSection("HOME and the Keychain", "Does a different `HOME` change which keychains are used, or `NSHomeDirectory`?")
	fake := filepath.Join(tmp, "home")
	_ = os.MkdirAll(fake, 0o700)
	env := func(home string) []string {
		var out []string
		for _, kv := range os.Environ() {
			if !strings.HasPrefix(kv, "HOME=") {
				out = append(out, kv)
			}
		}
		return append(out, "HOME="+home)
	}
	real := os.Getenv("HOME")
	for _, home := range []string{real, fake} {
		label := "HOME=" + strings.Replace(home, real, "$HOME", 1)
		if home == fake {
			label = "HOME=<temporary folder>"
		}
		out, code := run(10*time.Second, env(home), "/usr/bin/security", "default-keychain", "-d", "user")
		s.add(label, "security default-keychain", short(strings.ReplaceAll(out, real, "$HOME"), code))
		out, code = run(10*time.Second, env(home), "/usr/bin/security", "list-keychains", "-d", "user")
		s.add(label, "security list-keychains", short(strings.ReplaceAll(out, real, "$HOME"), code))
		out, code = run(20*time.Second, env(home), "/usr/bin/osascript", "-l", "JavaScript", "-e", `ObjC.import("Foundation"); $.NSHomeDirectory().js`)
		out = strings.ReplaceAll(strings.ReplaceAll(out, fake, "<temporary folder>"), real, "$HOME")
		s.add(label, "NSHomeDirectory()", short(out, code))
	}
}

// probeToolLogins says where the agent tools keep their logins on this Mac,
// without reading any secret.
func probeToolLogins() {
	s := newSection("Agent tools' logins", "Where do Claude Code, Codex and Chimera keep their logins here? (Existence only; nothing secret is read.)")
	home := os.Getenv("HOME")
	_, code := run(10*time.Second, nil, "/usr/bin/security", "find-generic-password", "-s", "Claude Code-credentials")
	s.add("Claude Code", "Keychain item `Claude Code-credentials`", map[bool]string{true: "present", false: fmt.Sprintf("absent (exit %d)", code)}[code == 0])
	for _, p := range []string{".claude/.credentials.json", ".codex/auth.json", ".chimera", ".config/chimera", ".local/share/chimera", "Library/Application Support/chimera"} {
		_, err := os.Stat(filepath.Join(home, p))
		s.add("", "`~/"+p+"`", map[bool]string{true: "present", false: "absent"}[err == nil])
	}
	if f, err := os.Open(filepath.Join(home, ".codex/config.toml")); err == nil {
		scan := bufio.NewScanner(f)
		found := "not set"
		for scan.Scan() {
			if line := strings.TrimSpace(scan.Text()); strings.HasPrefix(line, "cli_auth_credentials_store") {
				found = "`" + line + "`"
			}
		}
		f.Close()
		s.add("Codex", "`cli_auth_credentials_store` in `~/.codex/config.toml`", found)
	}
}

func probeProcArgs(self string) {
	s := newSection("Other processes", "Can a sandboxed process see, read the arguments and environment of, or signal a process outside it?")
	sleeper := exec.Command("/bin/sleep", "300", argMarker)
	sleeper.Env = append(os.Environ(), "LGPROBE_SECRET="+secretMarker)
	if err := sleeper.Start(); err != nil {
		s.note("could not start the outside process: %v", err)
		return
	}
	defer func() { _ = sleeper.Process.Kill(); _ = sleeper.Wait() }()
	pid := strconv.Itoa(sleeper.Process.Pid)
	same := `(deny process-info*)(allow process-info* (target same-sandbox))(deny signal)(allow signal (target same-sandbox))`
	variants := []struct{ name, rules string }{
		{"allow default", ""},
		{"process-info and signal same-sandbox", same},
		{"… and deny sysctl kern.procargs*", same + `(deny sysctl-read (sysctl-name-prefix "kern.procargs"))`},
	}
	// KERN_PROCARGS2 by its MIB, as ps reads it (ps itself is setuid, and a
	// sandbox runs no setuid program).
	procargs := `import ctypes, sys
libc = ctypes.CDLL(None, use_errno=True)
mib = (ctypes.c_int * 3)(1, 49, int(sys.argv[1]))
size = ctypes.c_size_t(1 << 20)
buf = ctypes.create_string_buffer(1 << 20)
if libc.sysctl(mib, 3, buf, ctypes.byref(size), None, 0) != 0:
    print("refused, errno", ctypes.get_errno()); sys.exit(1)
raw = buf.raw[:size.value]
print("arguments visible:", b"` + argMarker + `" in raw, "; environment visible:", b"` + secretMarker + `" in raw)
`
	python := lookPath("python3")
	if python != "" {
		out, code := run(30*time.Second, nil, python, "-I", "-c", procargs, pid)
		s.add("outside the sandbox", "sysctl {CTL_KERN, KERN_PROCARGS2, pid}", short(out, code))
	}
	out, code := run(30*time.Second, nil, "/usr/bin/pgrep", "-lf", argMarker)
	s.add("outside the sandbox", "pgrep -lf (lists it with its arguments?)", short(out, code))
	for _, v := range variants {
		if python != "" {
			out, code := sbx(allowDefault+v.rules, nil, python, "-I", "-c", procargs, pid)
			s.add(v.name, "sysctl {CTL_KERN, KERN_PROCARGS2, pid}", short(out, code))
		}
		out, code := sbx(allowDefault+v.rules, nil, "/usr/bin/pgrep", "-lf", argMarker)
		s.add(v.name, "pgrep -lf (lists it with its arguments?)", short(out, code))
		out, code = sbx(allowDefault+v.rules, nil, "/bin/kill", "-CONT", pid)
		s.add(v.name, "kill -CONT (a real signal)", short(out, code))
		out, code = sbx(allowDefault+v.rules, nil, "/bin/ps", "-E", "-ww", "-o", "command=", "-p", pid)
		s.add(v.name, "ps -E -p", map[bool]string{true: "**environment visible**", false: "environment hidden; "}[strings.Contains(out, secretMarker)]+short(out, code))
		out, code = sbx(allowDefault+v.rules, nil, "/bin/ps", "-ax", "-o", "pid=")
		listed := false
		for _, f := range strings.Fields(out) {
			listed = listed || f == pid
		}
		s.add(v.name, "ps -ax lists it", fmt.Sprintf("%v (exit %d)", listed, code))
	}
}

// probeSignals asks, under (deny default) as Legato's profile is, whether a
// sandboxed process can signal one outside it.
func probeSignals() {
	s := newSection("Signals under deny-default", "Under `(deny default)` with `(allow signal (target same-sandbox))`, can a sandboxed process end a process outside it?")
	base := "(version 1)\n(deny default)\n(allow process-exec)(allow process-fork)(allow file-read*)(allow file-map-executable)\n" + codexSysctls +
		`(allow mach-lookup (global-name "com.apple.system.opendirectoryd.libinfo"))` + "\n"
	variants := []struct{ name, rules string }{
		{"no signal rule", ""},
		{"signal same-sandbox", "(allow signal (target same-sandbox))"},
		{"signal others allowed", "(allow signal)"},
	}
	for _, v := range variants {
		victim := exec.Command("/bin/sleep", "300")
		if err := victim.Start(); err != nil {
			s.note("could not start a process: %v", err)
			return
		}
		pid := strconv.Itoa(victim.Process.Pid)
		out, code := sbx(base+v.rules, nil, "/bin/kill", "-TERM", pid)
		done := make(chan error, 1)
		go func() { done <- victim.Wait() }()
		alive := true
		select {
		case <-done:
			alive = false
		case <-time.After(time.Second):
			_ = victim.Process.Kill()
			<-done
		}
		s.add(v.name, "kill -TERM <outside pid>", fmt.Sprintf("outside process %s; %s", map[bool]string{true: "survived", false: "**was ended**"}[alive], short(out, code)))
	}
	out, code := sbx(base+"(allow signal (target same-sandbox))", nil, "/bin/sh", "-c", "sleep 30 & kill -TERM $! && echo ended-own-child")
	s.add("signal same-sandbox", "kill -TERM <its own child>", short(out, code))
}

// probeFirmlink asks whether a denied file can be read through the Data
// volume's path, which an allowed /System would hold.
func probeFirmlink(tmp string) {
	s := newSection("Firmlinks", "With `/System` allowed and a folder denied, can its file be read through `/System/Volumes/Data/…`?")
	dir := filepath.Join(tmp, "firm")
	_ = os.MkdirAll(dir, 0o700)
	key := filepath.Join(dir, "key")
	_ = os.WriteFile(key, []byte(secretMarker+"\n"), 0o600)
	profile := allowDefault + `(deny file-read* (subpath "/System/Volumes/Data"))(allow file-read* (subpath "/System"))(deny file-read* (subpath (param "D")))`
	for _, path := range []string{key, "/System/Volumes/Data" + key} {
		out, code := sbx(profile, []string{"D=" + dir}, "/bin/cat", path)
		s.add("allow /System, deny the folder", "cat "+strings.Replace(path, tmp, "<tmp>", 1), secretOutcome(out, code))
	}
	profile = allowDefault + `(allow file-read* (subpath "/System"))(deny file-read* (subpath (param "D")))`
	out, code := sbx(profile, []string{"D=" + dir}, "/bin/cat", "/System/Volumes/Data"+key)
	s.add("same, without denying /System/Volumes/Data", "cat /System/Volumes/Data<tmp>/firm/key", secretOutcome(out, code))
}

func probeLinks(self, tmp string) {
	s := newSection("Links, clones and renames", "With a folder denied, can a hard link, a clonefile copy, a symbolic link or a rename of its parent make its file readable?")
	root := filepath.Join(tmp, "links")
	parent := filepath.Join(root, "D")
	secret := filepath.Join(parent, "secret")
	work := filepath.Join(root, "W")
	reset := func() {
		_ = os.RemoveAll(root)
		_ = os.MkdirAll(secret, 0o700)
		_ = os.MkdirAll(work, 0o700)
		_ = os.WriteFile(filepath.Join(secret, "key"), []byte(secretMarker+"\n"), 0o600)
	}
	key := filepath.Join(secret, "key")
	params := []string{"S=" + secret, "D=" + parent}
	deny := `(deny file-read* file-write* (subpath (param "S")))`
	variants := []struct{ name, rules string }{
		{"deny the secret folder", deny},
		{"… and deny unlinking its parent", deny + `(deny file-write-unlink (require-all (vnode-type DIRECTORY) (literal (param "D"))))`},
		{"… and deny file-link, file-clone on it", deny + `(deny file-link (subpath (param "S")))(deny file-clone (subpath (param "S")))`},
	}
	type attempt struct {
		name string
		cmd  []string
	}
	attempts := []attempt{
		{"cat", []string{"/bin/cat", key}},
		{"stat", []string{"/usr/bin/stat", "-f", "%z", key}},
		{"hard link", []string{"/bin/sh", "-c", fmt.Sprintf("ln %q %q && cat %q", key, filepath.Join(work, "hl"), filepath.Join(work, "hl"))}},
		{"clonefile()", []string{self, "helper", "clonefile", key, filepath.Join(work, "cl")}},
		{"cp -c", []string{"/bin/sh", "-c", fmt.Sprintf("cp -c %q %q && cat %q", key, filepath.Join(work, "cp"), filepath.Join(work, "cp"))}},
		{"symbolic link", []string{"/bin/sh", "-c", fmt.Sprintf("ln -s %q %q && cat %q", key, filepath.Join(work, "sl"), filepath.Join(work, "sl"))}},
		{"rename the parent", []string{"/bin/sh", "-c", fmt.Sprintf("mv %q %q && cat %q", parent, parent+"2", filepath.Join(parent+"2", "secret", "key"))}},
	}
	for _, v := range variants {
		for _, a := range attempts {
			reset()
			out, code := sbx(allowDefault+v.rules, params, a.cmd...)
			s.add(v.name, a.name, secretOutcome(out, code))
		}
	}
	_ = os.RemoveAll(root)
}

func probeTIOCSTI(self string) {
	s := newSection("TIOCSTI", "Can a sandboxed process push input into its controlling terminal, which the shell that started it would read after it exits?")
	variants := []struct{ name, rules string }{
		{"allow default", ""},
		{"deny ioctl-command TIOCSTI (name)", `(deny file-ioctl (ioctl-command TIOCSTI))`},
		{"deny ioctl-command TIOCSTI (number)", fmt.Sprintf(`(deny file-ioctl (ioctl-command %d))`, uint32(unix.TIOCSTI))},
	}
	for _, v := range variants {
		out, err := inPTY(sandboxExec, "-p", allowDefault+v.rules, "--", self, "helper", "tiocsti")
		if err != nil {
			out += " (" + err.Error() + ")"
		}
		s.add(v.name, "ioctl(/dev/tty, TIOCSTI)", strings.TrimSpace(out))
	}
}

// inPTY runs a command as the session leader of a new terminal and returns
// what it wrote there.
func inPTY(name string, args ...string) (string, error) {
	master, slave, err := openPTY()
	if err != nil {
		return "", err
	}
	defer master.Close()
	cmd := exec.Command(name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		slave.Close()
		return "", err
	}
	slave.Close()
	var buf bytes.Buffer
	var mu sync.Mutex
	go func() {
		b := make([]byte, 4096)
		for {
			n, err := master.Read(b)
			mu.Lock()
			buf.Write(b[:n])
			mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err = <-done:
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		err = errors.New("timed out")
	}
	time.Sleep(200 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	return strings.ReplaceAll(buf.String(), "\r", ""), err
}

func openPTY() (master, slave *os.File, err error) {
	fd, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	master = os.NewFile(uintptr(fd), "/dev/ptmx")
	if err := unix.IoctlSetInt(fd, unix.TIOCPTYGRANT, 0); err != nil {
		master.Close()
		return nil, nil, err
	}
	if err := unix.IoctlSetInt(fd, unix.TIOCPTYUNLK, 0); err != nil {
		master.Close()
		return nil, nil, err
	}
	var name [128]byte
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.TIOCPTYGNAME), uintptr(unsafe.Pointer(&name[0]))); errno != 0 {
		master.Close()
		return nil, nil, errno
	}
	end := bytes.IndexByte(name[:], 0)
	if end < 0 {
		end = len(name)
	}
	slave, err = os.OpenFile(string(name[:end]), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		return nil, nil, err
	}
	return master, slave, nil
}

// codexSysctls is the sysctl allowlist of Codex's base policy
// (codex-rs/sandboxing/src/seatbelt_base_policy.sbpl).
const codexSysctls = `(allow sysctl-read
  (sysctl-name "hw.activecpu") (sysctl-name "hw.busfrequency_compat") (sysctl-name "hw.byteorder")
  (sysctl-name "hw.cacheconfig") (sysctl-name "hw.cachelinesize_compat") (sysctl-name "hw.cpufamily")
  (sysctl-name "hw.cpufrequency_compat") (sysctl-name "hw.cputype") (sysctl-name "hw.l1dcachesize_compat")
  (sysctl-name "hw.l1icachesize_compat") (sysctl-name "hw.l2cachesize_compat") (sysctl-name "hw.l3cachesize_compat")
  (sysctl-name "hw.logicalcpu_max") (sysctl-name "hw.machine") (sysctl-name "hw.model") (sysctl-name "hw.memsize")
  (sysctl-name "hw.ncpu") (sysctl-name "hw.nperflevels") (sysctl-name-prefix "hw.optional.arm.")
  (sysctl-name-prefix "hw.optional.armv8_") (sysctl-name "hw.packages") (sysctl-name "hw.pagesize_compat")
  (sysctl-name "hw.pagesize") (sysctl-name "hw.physicalcpu") (sysctl-name "hw.physicalcpu_max")
  (sysctl-name "hw.logicalcpu") (sysctl-name "hw.cpufrequency") (sysctl-name "hw.tbfrequency_compat")
  (sysctl-name "hw.vectorunit") (sysctl-name "machdep.cpu.brand_string") (sysctl-name "kern.argmax")
  (sysctl-name "kern.hostname") (sysctl-name "kern.maxfilesperproc") (sysctl-name "kern.maxproc")
  (sysctl-name "kern.osproductversion") (sysctl-name "kern.osrelease") (sysctl-name "kern.ostype")
  (sysctl-name "kern.osvariant_status") (sysctl-name "kern.osversion") (sysctl-name "kern.secure_kernel")
  (sysctl-name "kern.sysv.semmns") (sysctl-name "kern.usrstack64") (sysctl-name "kern.version")
  (sysctl-name "sysctl.proc_cputype") (sysctl-name "vm.loadavg") (sysctl-name-prefix "hw.perflevel")
  (sysctl-name-prefix "kern.proc.pgrp.") (sysctl-name-prefix "kern.proc.pid.") (sysctl-name-prefix "net.routetable."))
`

// zshSysctls are the names that zsh needs on macOS 26 under deny-default,
// as Gemini CLI and Cursor report.
const zshSysctls = `(allow sysctl-read (sysctl-name "hw.targettype") (sysctl-name "hw.osenvironment")
  (sysctl-name "hw.features.allows_security_research") (sysctl-name "hw.jetsam_properties_product_type"))
`

func probeShells() {
	s := newSection("Shells and tools under deny-default", "Which sysctls and services do the shells and common tools need under `(deny default)` with all reads allowed?")
	base := `(version 1)
(deny default (with message "legato-probe-shell"))
(allow process-exec)
(allow process-fork)
(allow signal (target same-sandbox))
(allow process-info* (target same-sandbox))
(allow file-read*)
(allow file-map-executable)
(allow file-write-data (literal "/dev/null"))
(allow file-ioctl (literal "/dev/null"))
(allow mach-lookup (global-name "com.apple.system.opendirectoryd.libinfo") (global-name "com.apple.system.logger") (global-name "com.apple.logd"))
(allow ipc-posix-sem)
`
	variants := []struct{ name, rules string }{
		{"Codex's sysctls", codexSysctls},
		{"Codex's + zsh's hw.* names", codexSysctls + zshSysctls},
		{"all sysctls", "(allow sysctl-read)\n"},
	}
	commands := [][]string{{"/bin/zsh", "-c", "echo ok"}, {"/bin/zsh", "-l", "-c", "echo ok"}, {"/bin/bash", "-c", "echo ok"},
		{"/bin/sh", "-c", "echo ok"}, {"/bin/ps", "-ax", "-o", "pid="}, {"/usr/bin/git", "--version"}}
	if goBin := lookPath("go"); goBin != "" {
		commands = append(commands, []string{goBin, "version"})
	}
	start := time.Now()
	for _, v := range variants {
		for _, c := range commands {
			out, code := sbx(base+v.rules, nil, c...)
			if c[0] == "/bin/ps" && code == 0 {
				out = fmt.Sprintf("%d processes listed", len(strings.Fields(out)))
			}
			s.add(v.name, strings.Join(c, " "), short(out, code))
		}
	}
	since := time.Since(start) + 30*time.Second
	out, _ := run(60*time.Second, nil, "/usr/bin/log", "show", "--style", "compact", "--last", fmt.Sprintf("%ds", int(since.Seconds())),
		"--predicate", `eventMessage CONTAINS "legato-probe-shell" OR (eventMessage CONTAINS "Sandbox:" AND eventMessage CONTAINS "deny")`)
	seen := map[string]bool{}
	var denials []string
	for _, line := range strings.Split(out, "\n") {
		i := strings.Index(line, "deny(")
		if i < 0 {
			continue
		}
		d := strings.TrimSpace(line[i:])
		if j := strings.Index(d, "("); j >= 0 {
			if k := strings.Index(d[j:], ")"); k >= 0 {
				d = strings.TrimSpace(d[j+k+1:])
			}
		}
		if !seen[d] {
			seen[d] = true
			denials = append(denials, d)
		}
	}
	sort.Strings(denials)
	if len(denials) > 80 {
		denials = append(denials[:80], "…")
	}
	if len(denials) == 0 {
		s.note("`log show` found no denials (%d bytes of output).", len(out))
	} else {
		s.note("Denials logged during these runs (all sandboxed processes):\n\n```\n%s\n```", strings.Join(denials, "\n"))
	}
}

func probeUnixSockets(self, tmp string) {
	s := newSection("Unix sockets", "Does `(remote unix-socket (subpath …))` allow one folder's sockets and refuse others? Does DNS need the mDNSResponder socket?")
	a, b := filepath.Join(tmp, "A"), filepath.Join(tmp, "B")
	_ = os.MkdirAll(a, 0o700)
	_ = os.MkdirAll(b, 0o700)
	for _, dir := range []string{a, b} {
		l, err := net.Listen("unix", filepath.Join(dir, "s.sock"))
		if err != nil {
			s.note("listen in %s: %v", dir, err)
			return
		}
		defer l.Close()
		go func() {
			for {
				c, err := l.Accept()
				if err != nil {
					return
				}
				_, _ = c.Write([]byte("hello\n"))
				c.Close()
			}
		}()
	}
	params := []string{"A=" + a, "SA=" + filepath.Join(a, "s.sock")}
	variants := []struct{ name, rules string }{
		{"deny all, allow subpath A", `(deny network-outbound (remote unix-socket))(allow network-outbound (remote unix-socket (subpath (param "A"))))`},
		{"deny all, allow path-literal A/s.sock", `(deny network-outbound (remote unix-socket))(allow network-outbound (remote unix-socket (path-literal (param "SA"))))`},
	}
	for _, v := range variants {
		for _, dir := range []string{a, b} {
			out, code := sbx(allowDefault+v.rules, params, self, "helper", "connect", filepath.Join(dir, "s.sock"))
			s.add(v.name, "connect "+filepath.Base(dir)+"/s.sock", short(out, code))
		}
	}
	dns := []struct{ name, rules string }{
		{"deny all unix sockets", `(deny network-outbound (remote unix-socket))`},
		{"… but mDNSResponder", `(deny network-outbound (remote unix-socket))(allow network-outbound (remote unix-socket (path-literal "/private/var/run/mDNSResponder")))`},
	}
	for _, v := range dns {
		out, code := sbx(allowDefault+v.rules, nil, self, "helper", "dns", "github.com")
		s.add(v.name, "Go LookupHost github.com", short(out, code))
		out, code = sbx(allowDefault+v.rules, nil, "/usr/bin/curl", "-sS", "-o", "/dev/null", "-w", "%{http_code}", "https://github.com")
		s.add(v.name, "curl https://github.com", short(out, code))
	}
}

func probeNested() {
	s := newSection("Nested sandboxes", "What happens when a sandboxed process applies a sandbox of its own (as Claude Code and Codex do)?")
	out, code := sbx(allowDefault, nil, sandboxExec, "-p", allowDefault, "/usr/bin/true")
	s.add("allow default inside allow default", "sandbox-exec … /usr/bin/true", short(out, code))
	strict := "(version 1)\n(deny default)\n(allow process-exec)(allow process-fork)(allow file-read*)(allow file-map-executable)(allow sysctl-read)\n"
	out, code = sbx(strict, nil, sandboxExec, "-p", allowDefault, "/usr/bin/true")
	s.add("allow default inside deny default", "sandbox-exec … /usr/bin/true", short(out, code))
}

func report() {
	var b strings.Builder
	b.WriteString("# Seatbelt probe (LEG-168)\n\n")
	vers, _ := run(10*time.Second, nil, "/usr/bin/sw_vers")
	fmt.Fprintf(&b, "- macOS: %s\n- Architecture: %s\n- Probe built with: %s\n- When: %s\n\n",
		strings.ReplaceAll(vers, "\n", ", "), runtime.GOARCH, runtime.Version(), time.Now().UTC().Format(time.RFC3339))
	for _, s := range sections {
		fmt.Fprintf(&b, "## %s\n\n%s\n\n", s.title, s.question)
		if len(s.rows) > 0 {
			b.WriteString("| Variant | Command | Outcome |\n| --- | --- | --- |\n")
			for _, r := range s.rows {
				fmt.Fprintf(&b, "| %s | %s | %s |\n", r.variant, r.command, r.outcome)
			}
			b.WriteString("\n")
		}
		for _, n := range s.notes {
			b.WriteString(n + "\n\n")
		}
	}
	fmt.Print(b.String())
}

// helper is what the probes run inside a sandbox.
func helper(name string, args []string) int {
	fail := func(err error) int {
		fmt.Println("error:", err)
		return 1
	}
	switch name {
	case "tls":
		client := http.Client{Timeout: 30 * time.Second}
		resp, err := client.Get(args[0])
		if err != nil {
			return fail(err)
		}
		resp.Body.Close()
		fmt.Println(resp.Status)
	case "clonefile":
		if err := unix.Clonefile(args[0], args[1], 0); err != nil {
			return fail(err)
		}
		data, err := os.ReadFile(args[1])
		if err != nil {
			return fail(err)
		}
		fmt.Print(string(data))
	case "tiocsti":
		tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
		if err != nil {
			return fail(err)
		}
		defer tty.Close()
		if err := unix.IoctlSetPointerInt(int(tty.Fd()), unix.TIOCSTI, int('#')); err != nil {
			fmt.Println("TIOCSTI refused:", err)
			return 1
		}
		fmt.Println("TIOCSTI ALLOWED: input was pushed into the terminal")
	case "connect":
		c, err := net.DialTimeout("unix", args[0], 5*time.Second)
		if err != nil {
			return fail(err)
		}
		defer c.Close()
		line, _ := bufio.NewReader(c).ReadString('\n')
		fmt.Println("connected:", strings.TrimSpace(line))
	case "dns":
		addrs, err := net.LookupHost(args[0])
		if err != nil {
			return fail(err)
		}
		fmt.Println(len(addrs), "addresses")
	default:
		return fail(fmt.Errorf("unknown helper %q", name))
	}
	return 0
}
