package sandbox

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// SandboxExec is macOS's sandbox-exec, always by this path: one planted on
// PATH would run first, and whoever can replace this one is root already.
const SandboxExec = "/usr/bin/sandbox-exec"

// Seatbelt is the macOS backend: sandbox-exec with a profile written from
// the plan. Seatbelt allows and denies paths and services; it cannot show a
// path elsewhere, an empty home or a throwaway layer, or hide processes
// (Capabilities is empty), so a plan for it shows every path where it is.
//
// The profile denies by default. It allows the plan's mounts in the plan's
// order (the last rule that matches wins, as bwrap's last mount covers),
// the network as Linux's sandbox does, the Unix sockets the plan names and
// DNS's, and a short list of system services; it keeps the Keychain
// (securityd), the pasteboard, launching and Apple Events, other
// processes, and typing into the terminal (TIOCSTI) out. Paths reach it
// only as parameters (-D), so none can add a rule. See docs/ai/sandbox.md.
type Seatbelt struct {
	// Launcher is the legato binary; its `sandbox exec` applies the
	// environment and working directory, then runs sandbox-exec.
	Launcher string
	// Exec is sandbox-exec; SandboxExec when empty.
	Exec string
	// GOOS is runtime.GOOS when empty.
	GOOS string
	// Try runs sandbox-exec with a profile that allows everything, to see
	// that it works here; running it when nil.
	Try func(exec string) error
}

func (s Seatbelt) Name() string { return "seatbelt" }

// Capabilities are none: Seatbelt only allows and denies.
func (s Seatbelt) Capabilities() Capabilities { return Capabilities{} }

func (s Seatbelt) exec() string {
	if s.Exec != "" {
		return s.Exec
	}
	return SandboxExec
}

func (s Seatbelt) Available() error {
	goos := s.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	if goos != "darwin" {
		return fmt.Errorf("the seatbelt sandbox runs on macOS only; on %s, use agents.sandbox.backend bwrap", goos)
	}
	if _, err := os.Stat(s.exec()); err != nil {
		return fmt.Errorf("%s is missing, and Legato's sandbox on macOS needs it: turn agents.sandbox.enabled off", s.exec())
	}
	try := s.Try
	if try == nil {
		try = func(binary string) error {
			out, err := exec.Command(binary, "-p", "(version 1)(allow default)", "/usr/bin/true").CombinedOutput()
			if err != nil {
				return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
			}
			return nil
		}
	}
	if err := try(s.exec()); err != nil {
		return fmt.Errorf("%s does not work here (%v); is Legato itself running in a sandbox?", s.exec(), err)
	}
	return nil
}

// LaunchSpec is what `legato sandbox exec` reads: how to start a command in
// the sandbox. It is written beside the profile, mode 0600.
type LaunchSpec struct {
	Exec     string            `json:"exec"`
	Profile  string            `json:"profile"`
	Params   []string          `json:"params"`
	Setenv   map[string]string `json:"setenv"`
	Unsetenv []string          `json:"unsetenv"`
	Chdir    string            `json:"chdir"`
}

// Argv is sandbox-exec's command line for command; tty is the terminal
// the command runs in (/dev/null without one), which the profile names as
// the TTY parameter.
func (l LaunchSpec) Argv(tty string, command []string) []string {
	if tty == "" {
		tty = "/dev/null"
	}
	argv := []string{l.Exec, "-f", l.Profile}
	for _, p := range l.Params {
		argv = append(argv, "-D"+p)
	}
	argv = append(argv, "-DTTY="+tty, "--")
	return append(argv, command...)
}

// Environ is env without what the spec unsets, with what it sets.
func (l LaunchSpec) Environ(env []string) []string {
	drop := map[string]bool{}
	for _, k := range l.Unsetenv {
		drop[k] = true
	}
	for k := range l.Setenv {
		drop[k] = true
	}
	var out []string
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if !drop[k] {
			out = append(out, kv)
		}
	}
	keys := make([]string, 0, len(l.Setenv))
	for k := range l.Setenv {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out = append(out, k+"="+l.Setenv[k])
	}
	return out
}

// ReadLaunchSpec reads the spec a Seatbelt Prefix wrote.
func ReadLaunchSpec(file string) (LaunchSpec, error) {
	var spec LaunchSpec
	data, err := os.ReadFile(file)
	if err != nil {
		return spec, err
	}
	if err := json.Unmarshal(data, &spec); err != nil {
		return spec, fmt.Errorf("%s: %w", file, err)
	}
	if spec.Exec == "" || spec.Profile == "" {
		return spec, fmt.Errorf("%s names no sandbox-exec or profile", file)
	}
	return spec, nil
}

// Prefix writes dir/seatbelt.sb and dir/seatbelt.json, makes the plan's
// links, and returns `<launcher> sandbox exec <dir>/seatbelt.json --`.
func (s Seatbelt) Prefix(plan Plan, dir string) (string, error) {
	if dir == "" {
		return "", errors.New("the seatbelt sandbox needs the execution's runtime folder")
	}
	if s.Launcher == "" {
		return "", errors.New("the seatbelt sandbox needs the legato binary to launch through")
	}
	profile, params, err := s.Profile(plan, filepath.Base(dir))
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	for _, l := range plan.Links {
		if err := makeLink(l); err != nil {
			return "", err
		}
	}
	profileFile, specFile := filepath.Join(dir, "seatbelt.sb"), filepath.Join(dir, "seatbelt.json")
	if err := os.WriteFile(profileFile, []byte(profile), 0o600); err != nil {
		return "", err
	}
	spec := LaunchSpec{Exec: s.exec(), Profile: profileFile, Params: params, Setenv: plan.Setenv, Unsetenv: plan.Unsetenv, Chdir: plan.Chdir}
	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(specFile, data, 0o600); err != nil {
		return "", err
	}
	return ShellWord(s.Launcher) + " sandbox exec " + ShellWord(specFile) + " --", nil
}

// makeLink puts a link at l.Path, replacing a file or link there (an
// empty placeholder), never a folder.
func makeLink(l Link) error {
	if !filepath.IsAbs(l.Path) || !filepath.IsAbs(l.Target) {
		return fmt.Errorf("link %s -> %s is not absolute", l.Path, l.Target)
	}
	if err := os.MkdirAll(filepath.Dir(l.Path), 0o700); err != nil {
		return err
	}
	if info, err := os.Lstat(l.Path); err == nil {
		if info.IsDir() {
			return fmt.Errorf("%s is a folder, not a link", l.Path)
		}
		if err := os.Remove(l.Path); err != nil {
			return err
		}
	}
	return os.Symlink(l.Target, l.Path)
}

// profileBuilder names each path once, as a parameter.
type profileBuilder struct {
	b      strings.Builder
	names  map[string]string
	params []string
}

func (p *profileBuilder) param(path string) string {
	if p.names == nil {
		p.names = map[string]string{}
	}
	name, ok := p.names[path]
	if !ok {
		name = fmt.Sprintf("P%d", len(p.params))
		p.names[path] = name
		p.params = append(p.params, name+"="+path)
	}
	return `(param "` + name + `")`
}

func (p *profileBuilder) line(format string, args ...any) {
	fmt.Fprintf(&p.b, format+"\n", args...)
}

// Profile is the SBPL profile of plan and its parameters (KEY=VALUE), the
// denials tagged legato:<tag>. The TTY parameter is the launcher's.
func (s Seatbelt) Profile(plan Plan, tag string) (string, []string, error) {
	var p profileBuilder
	p.line("(version 1)")
	p.line(`(deny default (with message "legato:%s"))`, strings.ReplaceAll(tag, `"`, ""))
	p.b.WriteString(seatbeltBase)

	var writable []string
	allow := func(path string, write bool) {
		ops := "file-read* process-exec file-map-executable"
		if write {
			ops = "file-read* file-write* process-exec file-map-executable"
		}
		p.line("(allow %s (subpath %s))", ops, p.param(path))
		if write {
			// Sockets the agent makes in its own folders.
			p.line("(allow network-bind (local unix-socket (subpath %s)))", p.param(path))
			p.line("(allow network-outbound (remote unix-socket (subpath %s)))", p.param(path))
			writable = append(writable, path)
		} else {
			p.line("(deny file-write* (subpath %s))", p.param(path))
		}
	}

	p.line("\n; The agent's own home.")
	if plan.HomeSource != "" {
		if !filepath.IsAbs(plan.HomeSource) {
			return "", nil, fmt.Errorf("home source %q is not absolute", plan.HomeSource)
		}
		allow(plan.HomeSource, true)
	}
	p.line("\n; The plan, in its order: the last rule that matches wins.")
	var covers []string
	for _, m := range plan.Mounts {
		if !filepath.IsAbs(m.Path) {
			return "", nil, fmt.Errorf("mount %q is not an absolute path", m.Path)
		}
		if m.Dest != "" && m.Dest != m.Path {
			return "", nil, fmt.Errorf("the seatbelt sandbox cannot show %s at %s", m.Path, m.Dest)
		}
		switch m.Kind {
		case ReadOnly:
			allow(m.Path, false)
			covers = append(covers, m.Path)
		case ReadWrite:
			allow(m.Path, true)
		case Symlink:
			// The host has the link; Seatbelt checks where it leads.
		case Hide, HideDir:
			p.line("(deny file-read* file-write* (subpath %s))", p.param(m.Path))
			covers = append(covers, m.Path)
		case Overlay:
			return "", nil, fmt.Errorf("the seatbelt sandbox cannot show %s beneath a throwaway layer", m.Path)
		default:
			return "", nil, fmt.Errorf("mount %q has an unknown kind", m.Path)
		}
	}

	p.line("\n; The Unix sockets the agent may reach.")
	for _, sock := range plan.Sockets {
		if !filepath.IsAbs(sock) {
			return "", nil, fmt.Errorf("socket %q is not absolute", sock)
		}
		p.line("(allow network-outbound (remote unix-socket (subpath %s)))", p.param(sock))
	}

	// A folder inside writable work that holds a read-only or hidden path
	// cannot be renamed: what it holds would be at a path no rule names.
	protected := map[string]bool{}
	for _, c := range covers {
		for _, w := range writable {
			if c == w || !within(c, w) {
				continue
			}
			for dir := c; within(dir, w); dir = filepath.Dir(dir) {
				protected[dir] = true
			}
		}
	}
	if len(protected) > 0 {
		p.line("\n; What holds a cover cannot be renamed.")
		dirs := make([]string, 0, len(protected))
		for dir := range protected {
			dirs = append(dirs, dir)
		}
		sort.Strings(dirs)
		for _, dir := range dirs {
			p.line("(deny file-write-unlink (require-all (vnode-type DIRECTORY) (literal %s)))", p.param(dir))
		}
	}
	p.b.WriteString(seatbeltLast)
	return p.b.String(), p.params, nil
}

func within(path, dir string) bool {
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}

// seatbeltBase is what every agent needs from the system, after
// Codex's base policy (codex-rs/sandboxing/src/seatbelt_base_policy.sbpl)
// and what the probe found zsh, git and Go need (LEG-168).
const seatbeltBase = `
; Processes: anything the sandbox can read runs; only its own processes
; can be signalled, inspected or debugged.
(allow process-fork)
(allow signal (target same-sandbox))
(allow process-info* (target same-sandbox))
(allow mach-priv-task-port (target same-sandbox))

; What programs read of the system.
(allow sysctl-read
  (sysctl-name "hw.activecpu") (sysctl-name "hw.busfrequency_compat") (sysctl-name "hw.byteorder")
  (sysctl-name "hw.cacheconfig") (sysctl-name "hw.cachelinesize_compat") (sysctl-name "hw.cpufamily")
  (sysctl-name "hw.cpufrequency") (sysctl-name "hw.cpufrequency_compat") (sysctl-name "hw.cpufrequency_max")
  (sysctl-name "hw.cputype") (sysctl-name "hw.l1dcachesize_compat") (sysctl-name "hw.l1icachesize_compat")
  (sysctl-name "hw.l2cachesize_compat") (sysctl-name "hw.l3cachesize_compat") (sysctl-name "hw.logicalcpu")
  (sysctl-name "hw.logicalcpu_max") (sysctl-name "hw.machine") (sysctl-name "hw.model") (sysctl-name "hw.memsize")
  (sysctl-name "hw.ncpu") (sysctl-name "hw.nperflevels") (sysctl-name "hw.packages") (sysctl-name "hw.pagesize")
  (sysctl-name "hw.pagesize_compat") (sysctl-name "hw.physicalcpu") (sysctl-name "hw.physicalcpu_max")
  (sysctl-name "hw.tbfrequency_compat") (sysctl-name "hw.vectorunit") (sysctl-name "hw.target")
  (sysctl-name "hw.targettype") (sysctl-name "hw.osenvironment") (sysctl-name "hw.features.allows_security_research")
  (sysctl-name "hw.jetsam_properties_product_type") (sysctl-name-prefix "hw.optional.") (sysctl-name-prefix "hw.perflevel")
  (sysctl-name "machdep.cpu.brand_string") (sysctl-name "kern.argmax") (sysctl-name "kern.hostname")
  (sysctl-name "kern.maxfilesperproc") (sysctl-name "kern.maxproc") (sysctl-name "kern.ngroups")
  (sysctl-name "kern.osproductversion") (sysctl-name "kern.osrelease") (sysctl-name "kern.ostype")
  (sysctl-name "kern.osvariant_status") (sysctl-name "kern.osversion") (sysctl-name "kern.secure_kernel")
  (sysctl-name "kern.sysv.semmns") (sysctl-name "kern.usrstack64") (sysctl-name "kern.version")
  (sysctl-name "kern.hv_vmm_present") (sysctl-name "kern.iossupportversion") (sysctl-name "security.mac.lockdown_mode_state")
  (sysctl-name "sysctl.proc_cputype") (sysctl-name "vm.loadavg")
  (sysctl-name-prefix "kern.proc.pgrp.") (sysctl-name-prefix "kern.proc.pid.") (sysctl-name-prefix "net.routetable."))
(allow sysctl-write (sysctl-name "kern.grade_cputype"))
(allow iokit-open (iokit-registry-entry-class "RootDomainUserClient"))
(allow iokit-get-properties)
(allow ipc-posix-sem)
(allow ipc-posix-shm)

; Devices and the terminal: the pane's (TTY, from the launcher), /dev/tty,
; and the pseudo-terminals the sandbox opens.
(allow file-read* file-write-data file-ioctl (literal "/dev/null") (literal "/dev/zero") (literal "/dev/dtracehelper"))
(allow file-read* (literal "/dev/random") (literal "/dev/urandom") (literal "/dev/autofs_nowait"))
(allow file-read* file-write-data (subpath "/dev/fd"))
(allow pseudo-tty)
(allow file-read* file-write* file-ioctl (literal "/dev/ptmx") (literal "/dev/tty") (literal (param "TTY")))
(allow file-read* file-write* file-ioctl (require-all (regex #"^/dev/ttys[0-9]+$") (extension "com.apple.sandbox.pty")))

; Every path's metadata, so stat and realpath work; never its content, and
; a cover below denies even that.
(allow file-read-metadata)
(allow file-read* (literal "/"))

; System services: users and groups, logging, notifications, certificate
; trust (Go needs trustd), network settings, preferences.
(allow mach-lookup
  (global-name "com.apple.system.opendirectoryd.libinfo") (global-name "com.apple.system.opendirectoryd.membership")
  (global-name "com.apple.system.logger") (global-name "com.apple.logd") (global-name "com.apple.diagnosticd")
  (global-name "com.apple.system.notification_center") (global-name "com.apple.bsd.dirhelper")
  (global-name "com.apple.trustd") (global-name "com.apple.trustd.agent") (global-name "com.apple.ocspd")
  (global-name "com.apple.SystemConfiguration.configd") (global-name "com.apple.SystemConfiguration.DNSConfiguration")
  (global-name "com.apple.networkd") (global-name "com.apple.FSEvents") (global-name "com.apple.PowerManagement.control")
  (global-name "com.apple.cfprefsd.daemon") (global-name "com.apple.cfprefsd.agent") (local-name "com.apple.cfprefsd.agent"))
(allow ipc-posix-shm-read* (ipc-posix-name-prefix "apple.cfprefs."))
(allow user-preference-read (preference-domain "kCFPreferencesAnyApplication") (preference-domain "com.apple.security"))

; The network, as Linux's sandbox has it: the host's, unfiltered. DNS goes
; through mDNSResponder's socket.
(allow network-outbound (remote ip))
(allow network-inbound (local ip))
(allow network-bind (local ip))
(allow system-socket (socket-domain AF_UNIX))
(allow system-socket (require-all (socket-domain AF_SYSTEM) (socket-protocol 2)))
(allow network-outbound (remote unix-socket (path-literal "/private/var/run/mDNSResponder")))
`

// seatbeltLast are the denials that hold whatever came before.
const seatbeltLast = `
; Never, whatever came before: the Keychain (securityd), the pasteboard,
; starting anything outside the sandbox (launchd jobs, LaunchServices,
; Apple Events), other processes' arguments and environment, changing a
; file through a read-only descriptor, setuid, and typing into the
; terminal, which the pane's shell would read after the agent exits.
(deny mach-lookup (xpc-service-name-prefix ""))
(deny mach-lookup
  (global-name "com.apple.SecurityServer") (global-name "com.apple.securityd.xpc") (global-name "com.apple.pasteboard.1")
  (global-name "com.apple.coreservices.launchservicesd") (global-name "com.apple.coreservices.appleevents")
  (global-name "com.apple.lsd.modifydb"))
(deny job-creation)
(deny lsopen)
(deny appleevent-send)
(deny sysctl-read (sysctl-name-prefix "kern.procargs"))
(deny system-fcntl (fcntl-command 80 110))
(deny file-write-setugid)
(deny process-exec
  (literal "/usr/bin/osascript") (literal "/usr/bin/open") (literal "/bin/launchctl") (literal "/usr/bin/security")
  (literal "/usr/bin/pbcopy") (literal "/usr/bin/pbpaste") (literal "/usr/sbin/screencapture") (literal "/usr/bin/crontab")
  (literal "/usr/bin/at") (literal "/usr/bin/sudo") (literal "/usr/bin/su") (literal "/usr/bin/login"))
(deny file-ioctl (ioctl-command TIOCSTI))
`
