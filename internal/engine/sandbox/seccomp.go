package sandbox

import (
	"encoding/binary"
	"fmt"
)

// TerminalFilter is a seccomp program, for bwrap's --seccomp, that refuses
// the ioctls that push input into a terminal (TIOCSTI) or reach the console
// (TIOCLINUX) with EPERM and allows every other system call. A sandboxed
// agent keeps the pane's terminal as its own (its size changes, /dev/tty),
// but cannot type into it: what it pushed there would be read by the pane's
// host shell after the agent exits. Linux 6.2 and later refuse TIOCSTI
// unless dev.tty.legacy_tiocsti is set; this refuses it everywhere, as
// Flatpak does.
//
// goarch is runtime.GOARCH: amd64 (with i386 and x32 system calls) or arm64
// (with 32-bit ARM ones). The request is compared in its low 32 bits, as the
// kernel truncates it, so high bits cannot slip past.
func TerminalFilter(goarch string) ([]byte, error) {
	type abi struct {
		arch  uint32
		ioctl []uint32
	}
	var abis []abi
	switch goarch {
	case "amd64":
		abis = []abi{{auditArchX8664, []uint32{16, x32Bit | 514}}, {auditArchI386, []uint32{54}}}
	case "arm64":
		abis = []abi{{auditArchAArch64, []uint32{29}}, {auditArchARM, []uint32{54}}}
	default:
		return nil, fmt.Errorf("no terminal filter for %s", goarch)
	}
	var p program
	for i, a := range abis {
		next := fmt.Sprintf("abi%d", i+1)
		if i == len(abis)-1 {
			next = "allow"
		}
		p.label(fmt.Sprintf("abi%d", i))
		p.load(offsetArch)
		p.jumpIf(a.arch, "", next)
		p.load(offsetNr)
		for j, nr := range a.ioctl {
			otherwise := ""
			if j == len(a.ioctl)-1 {
				otherwise = "allow"
			}
			p.jumpIf(nr, "request", otherwise)
		}
	}
	p.label("request")
	p.load(offsetArg1Low)
	p.jumpIf(tiocsti, "deny", "")
	p.jumpIf(tioclinux, "deny", "allow")
	p.label("allow")
	p.ret(seccompRetAllow)
	p.label("deny")
	p.ret(seccompRetErrno | eperm)
	return p.assemble()
}

const (
	auditArchX8664   = 0xc000003e
	auditArchI386    = 0x40000003
	auditArchAArch64 = 0xc00000b7
	auditArchARM     = 0x40000028
	x32Bit           = 0x40000000

	tiocsti   = 0x5412
	tioclinux = 0x541c

	seccompRetAllow = 0x7fff0000
	seccompRetErrno = 0x00050000
	eperm           = 1

	// struct seccomp_data: int nr; u32 arch; u64 ip; u64 args[6]. Both
	// architectures are little-endian, so args[1]'s low half is at 24.
	offsetNr      = 0
	offsetArch    = 4
	offsetArg1Low = 24

	bpfLdWAbs = 0x20 // BPF_LD | BPF_W | BPF_ABS
	bpfJeqK   = 0x15 // BPF_JMP | BPF_JEQ | BPF_K
	bpfRetK   = 0x06 // BPF_RET | BPF_K
)

// program is classic BPF with labels; a jump to "" goes to the next
// instruction.
type program struct {
	insns  []insn
	labels map[string]int
}

type insn struct {
	code   uint16
	k      uint32
	jt, jf string
}

func (p *program) label(name string) {
	if p.labels == nil {
		p.labels = map[string]int{}
	}
	p.labels[name] = len(p.insns)
}

func (p *program) load(offset uint32) { p.insns = append(p.insns, insn{code: bpfLdWAbs, k: offset}) }
func (p *program) ret(value uint32)   { p.insns = append(p.insns, insn{code: bpfRetK, k: value}) }
func (p *program) jumpIf(value uint32, then, otherwise string) {
	p.insns = append(p.insns, insn{code: bpfJeqK, k: value, jt: then, jf: otherwise})
}

// assemble resolves the labels (jumps go forward only, at most 255) and
// writes struct sock_filter, little-endian.
func (p *program) assemble() ([]byte, error) {
	offset := func(from int, to string) (uint8, error) {
		if to == "" {
			return 0, nil
		}
		at, ok := p.labels[to]
		if !ok || at <= from || at-from-1 > 255 {
			return 0, fmt.Errorf("bad jump from %d to %q", from, to)
		}
		return uint8(at - from - 1), nil
	}
	out := make([]byte, 0, 8*len(p.insns))
	for i, in := range p.insns {
		jt, err := offset(i, in.jt)
		if err != nil {
			return nil, err
		}
		jf, err := offset(i, in.jf)
		if err != nil {
			return nil, err
		}
		out = binary.LittleEndian.AppendUint16(out, in.code)
		out = append(out, jt, jf)
		out = binary.LittleEndian.AppendUint32(out, in.k)
	}
	return out, nil
}
