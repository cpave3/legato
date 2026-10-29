package credential

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// The operations of a push, one for each ref it updates.
const (
	OperationGitPush        = "git.push"
	OperationGitPushDefault = "git.push-default"
	OperationGitPushTag     = "git.push-tag"
	OperationGitDeleteRef   = "git.delete-ref"
	OperationGitRead        = "git.read"
)

// RefUpdate is one command of a push: a ref, from one object to another.
type RefUpdate struct {
	Old, New, Ref string
}

// ReceivePack is what a git-receive-pack request says before its pack.
type ReceivePack struct {
	Commands     []RefUpdate
	Capabilities []string
	Options      []string
}

// Has reports whether the client asked for a capability.
func (p ReceivePack) Has(capability string) bool {
	for _, c := range p.Capabilities {
		if c == capability || strings.HasPrefix(c, capability+"=") {
			return true
		}
	}
	return false
}

// maxPushHead bounds what is read of a push before its pack.
const maxPushHead = 1 << 20

// ReadReceivePack reads the commands, capabilities and push options at the
// start of a git-receive-pack request body, stopping before the pack. It
// returns the raw bytes it consumed from r, so the request can be passed on
// as it came: those bytes, then the rest of r. encoding is the request's
// Content-Encoding ("" or "gzip"). Anything it cannot read is an error,
// never a guess.
func ReadReceivePack(r io.Reader, encoding string) (ReceivePack, []byte, error) {
	var raw bytes.Buffer
	tee := io.TeeReader(io.LimitReader(r, maxPushHead), &raw)
	var src io.Reader = tee
	switch encoding {
	case "", "identity":
	case "gzip":
		z, err := gzip.NewReader(tee)
		if err != nil {
			return ReceivePack{}, nil, fmt.Errorf("the push is not gzip: %w", err)
		}
		src = z
	default:
		return ReceivePack{}, nil, fmt.Errorf("a push encoded as %q", encoding)
	}
	pkts := bufio.NewReaderSize(src, 4)
	var push ReceivePack
	idLen := 0
	for {
		line, flush, err := readPktLine(pkts)
		if err != nil {
			return ReceivePack{}, nil, err
		}
		if flush {
			break
		}
		if len(push.Commands) == 0 {
			if strings.HasPrefix(line, "shallow ") {
				continue
			}
			if strings.HasPrefix(line, "push-cert") {
				return ReceivePack{}, nil, errors.New("signed pushes are not taken")
			}
			cmd, caps, _ := strings.Cut(line, "\x00")
			line = cmd
			push.Capabilities = strings.Fields(caps)
		}
		update, err := parseRefUpdate(strings.TrimSuffix(line, "\n"))
		if err != nil {
			return ReceivePack{}, nil, err
		}
		if idLen == 0 {
			idLen = len(update.Old)
		} else if len(update.Old) != idLen {
			return ReceivePack{}, nil, errors.New("the push mixes object ID lengths")
		}
		push.Commands = append(push.Commands, update)
	}
	if len(push.Commands) == 0 {
		return ReceivePack{}, nil, errors.New("the push updates no ref")
	}
	if push.Has("push-options") {
		for {
			line, flush, err := readPktLine(pkts)
			if err != nil {
				return ReceivePack{}, nil, err
			}
			if flush {
				break
			}
			push.Options = append(push.Options, strings.TrimSuffix(line, "\n"))
		}
	}
	// The reader may have taken more than it parsed; that is fine, as the
	// caller passes on every byte read and then the rest.
	return push, raw.Bytes(), nil
}

// readPktLine reads one pkt-line: a payload, or a flush (0000).
func readPktLine(r *bufio.Reader) (string, bool, error) {
	var size [4]byte
	if _, err := io.ReadFull(r, size[:]); err != nil {
		return "", false, errors.New("the push ends before its commands do")
	}
	n, err := strconv.ParseUint(string(size[:]), 16, 16)
	if err != nil {
		return "", false, errors.New("the push has a malformed pkt-line")
	}
	if n == 0 {
		return "", true, nil
	}
	if n < 4 || n > 65520 {
		return "", false, errors.New("the push has a pkt-line of a length git does not send")
	}
	payload := make([]byte, n-4)
	if _, err := io.ReadFull(r, payload); err != nil {
		return "", false, errors.New("the push ends inside a pkt-line")
	}
	return string(payload), false, nil
}

func parseRefUpdate(line string) (RefUpdate, error) {
	parts := strings.SplitN(line, " ", 3)
	if len(parts) != 3 || parts[2] == "" {
		return RefUpdate{}, errors.New("the push has a command that is not <old> <new> <ref>")
	}
	for _, id := range parts[:2] {
		if (len(id) != 40 && len(id) != 64) || strings.Trim(id, "0123456789abcdef") != "" {
			return RefUpdate{}, fmt.Errorf("the push has an object ID git does not send: %q", id)
		}
	}
	if len(parts[0]) != len(parts[1]) {
		return RefUpdate{}, errors.New("the push mixes object ID lengths")
	}
	if strings.ContainsAny(parts[2], " \n\x00\t") || !strings.HasPrefix(parts[2], "refs/") {
		return RefUpdate{}, fmt.Errorf("the push names a ref git does not send: %q", parts[2])
	}
	return RefUpdate{Old: parts[0], New: parts[1], Ref: parts[2]}, nil
}

func allZero(id string) bool { return strings.Trim(id, "0") == "" }

// ClassifyRefUpdate says what one ref update of a push to repo
// (owner/name) does, given the repository's default branch.
func ClassifyRefUpdate(repo, defaultBranch string, u RefUpdate) (Classified, error) {
	out := Classified{Resource: "github:" + repo + "@" + u.Ref, Amounts: map[string]int64{"requests": 1}}
	switch {
	case allZero(u.New):
		out.Operation = OperationGitDeleteRef
	case strings.HasPrefix(u.Ref, "refs/tags/"):
		out.Operation = OperationGitPushTag
	case defaultBranch != "" && u.Ref == "refs/heads/"+defaultBranch:
		out.Operation = OperationGitPushDefault
	case strings.HasPrefix(u.Ref, "refs/heads/"):
		out.Operation = OperationGitPush
	default:
		return Classified{}, fmt.Errorf("%s is neither a branch nor a tag", u.Ref)
	}
	return out, nil
}

// RefusePush writes a git-receive-pack answer that refuses every ref of a
// push, in terms git prints: messages on side band 2 (shown as "remote:"),
// and an "ng" for each ref in the report. reasons are by ref; refs without
// one were refused with the rest, as a push is all or nothing.
func RefusePush(push ReceivePack, reasons map[string]string, messages []string) []byte {
	var report bytes.Buffer
	report.WriteString(pktLine("unpack ok\n"))
	for _, c := range push.Commands {
		reason := reasons[c.Ref]
		if reason == "" {
			reason = "refused with the rest of the push"
		}
		report.WriteString(pktLine("ng " + c.Ref + " " + strings.ReplaceAll(reason, "\n", " ") + "\n"))
	}
	report.WriteString("0000")
	if !push.Has("side-band-64k") && !push.Has("side-band") {
		return report.Bytes()
	}
	var out bytes.Buffer
	for _, m := range messages {
		out.WriteString(pktLine("\x02" + m + "\n"))
	}
	out.WriteString(pktLine("\x01" + report.String()))
	out.WriteString("0000")
	return out.Bytes()
}

func pktLine(payload string) string {
	return fmt.Sprintf("%04x%s", len(payload)+4, payload)
}
