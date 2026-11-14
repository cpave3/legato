package sandbox

import (
	"encoding/binary"
	"testing"
)

// run interprets the classic BPF of a seccomp filter (the loads, equality
// jumps and returns TerminalFilter uses) against one system call.
func runFilter(t *testing.T, filter []byte, arch, nr uint32, arg1 uint64) uint32 {
	t.Helper()
	data := make([]byte, 64)
	binary.LittleEndian.PutUint32(data[0:], nr)
	binary.LittleEndian.PutUint32(data[4:], arch)
	binary.LittleEndian.PutUint64(data[24:], arg1)
	var a uint32
	for pc := 0; pc*8 < len(filter); pc++ {
		in := filter[pc*8:]
		code, jt, jf, k := binary.LittleEndian.Uint16(in), in[2], in[3], binary.LittleEndian.Uint32(in[4:])
		switch code {
		case bpfLdWAbs:
			a = binary.LittleEndian.Uint32(data[k:])
		case bpfJeqK:
			if a == k {
				pc += int(jt)
			} else {
				pc += int(jf)
			}
		case bpfRetK:
			return k
		default:
			t.Fatalf("unexpected instruction %#x at %d", code, pc)
		}
	}
	t.Fatal("the filter ran off its end")
	return 0
}

func TestTheTerminalFilterRefusesOnlyTerminalInjection(t *testing.T) {
	const deny, allow = seccompRetErrno | eperm, seccompRetAllow
	cases := []struct {
		goarch   string
		arch, nr uint32
		request  uint64
		want     uint32
	}{
		{"amd64", auditArchX8664, 16, tiocsti, deny},
		{"amd64", auditArchX8664, 16, 1<<32 | tiocsti, deny},
		{"amd64", auditArchX8664, 16, tioclinux, deny},
		{"amd64", auditArchX8664, 16, 0x5413, allow}, // TIOCGWINSZ
		{"amd64", auditArchX8664, x32Bit | 514, tiocsti, deny},
		{"amd64", auditArchI386, 54, tiocsti, deny},
		{"amd64", auditArchI386, 16, tiocsti, allow}, // not ioctl on i386
		{"amd64", auditArchX8664, 0, tiocsti, allow}, // read(fd, buf, 0x5412)
		{"arm64", auditArchAArch64, 29, tiocsti, deny},
		{"arm64", auditArchAArch64, 29, 1<<40 | tiocsti, deny},
		{"arm64", auditArchARM, 54, tioclinux, deny},
		{"arm64", auditArchAArch64, 54, tiocsti, allow},
		{"arm64", auditArchX8664, 16, tiocsti, allow}, // not this machine's
	}
	for _, c := range cases {
		filter, err := TerminalFilter(c.goarch)
		if err != nil {
			t.Fatal(err)
		}
		if got := runFilter(t, filter, c.arch, c.nr, c.request); got != c.want {
			t.Errorf("%s arch %#x nr %d request %#x: %#x, want %#x", c.goarch, c.arch, c.nr, c.request, got, c.want)
		}
	}
	if _, err := TerminalFilter("riscv64"); err == nil {
		t.Error("a filter for an architecture it does not know")
	}
}
