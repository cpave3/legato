package credential

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"strings"
	"testing"
)

const (
	zeroID = "0000000000000000000000000000000000000000"
	oldID  = "1111111111111111111111111111111111111111"
	newID  = "2222222222222222222222222222222222222222"
)

func pkt(s string) string { return fmt.Sprintf("%04x%s", len(s)+4, s) }

// pushBody is a receive-pack request as git sends it: the commands, a
// flush, optional push options and a flush, then the pack.
func pushBody(caps string, options []string, cmds ...string) string {
	var b strings.Builder
	for i, c := range cmds {
		if i == 0 {
			c += "\x00" + caps
		}
		b.WriteString(pkt(c + "\n"))
	}
	b.WriteString("0000")
	if len(options) > 0 {
		for _, o := range options {
			b.WriteString(pkt(o + "\n"))
		}
		b.WriteString("0000")
	}
	b.WriteString("PACK\x00\x00\x00\x02rest-of-the-pack")
	return b.String()
}

func TestAReceivePackRequestIsReadUpToItsPack(t *testing.T) {
	body := pushBody("report-status side-band-64k push-options agent=git/2.47", []string{"ci.skip"},
		oldID+" "+newID+" refs/heads/legato/x", zeroID+" "+newID+" refs/heads/legato/y")
	push, head, err := ReadReceivePack(strings.NewReader(body), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(push.Commands) != 2 || push.Commands[0].Ref != "refs/heads/legato/x" || push.Commands[0].Old != oldID || push.Commands[1].New != newID {
		t.Fatalf("commands = %+v", push.Commands)
	}
	if !push.Has("side-band-64k") || !push.Has("report-status") || push.Has("atomic") {
		t.Fatalf("capabilities = %v", push.Capabilities)
	}
	if len(push.Options) != 1 || push.Options[0] != "ci.skip" {
		t.Fatalf("options = %v", push.Options)
	}
	// What was read, then the rest of the stream, is the request as it came.
	if !strings.HasPrefix(body, string(head)) || strings.Contains(string(head), "rest-of-the-pack") {
		t.Fatalf("head = %q", head)
	}
}

func TestAGzipBodyIsReadAndKeptAsItCame(t *testing.T) {
	plain := pushBody("report-status", nil, oldID+" "+newID+" refs/heads/legato/x")
	var zipped bytes.Buffer
	w := gzip.NewWriter(&zipped)
	_, _ = io.WriteString(w, plain)
	_ = w.Close()
	raw := zipped.Bytes()
	reader := bytes.NewReader(raw)
	push, head, err := ReadReceivePack(reader, "gzip")
	if err != nil || len(push.Commands) != 1 {
		t.Fatalf("push = %+v, %v", push, err)
	}
	rest, _ := io.ReadAll(reader)
	if !bytes.Equal(append(head, rest...), raw) {
		t.Fatal("the bytes read and the rest are not the request as it came")
	}
}

func TestSHA256ObjectIDsAreRead(t *testing.T) {
	old, nw := strings.Repeat("a", 64), strings.Repeat("b", 64)
	push, _, err := ReadReceivePack(strings.NewReader(pushBody("report-status object-format=sha256", nil, old+" "+nw+" refs/heads/legato/x")), "")
	if err != nil || push.Commands[0].New != nw {
		t.Fatalf("push = %+v, %v", push, err)
	}
}

func TestMalformedPushesAreRefused(t *testing.T) {
	for name, body := range map[string]string{
		"no commands":        "0000PACK",
		"short ID":           pushBody("", nil, "1234 "+newID+" refs/heads/x"),
		"mixed ID lengths":   pushBody("", nil, oldID+" "+strings.Repeat("b", 64)+" refs/heads/x"),
		"not hex":            pushBody("", nil, strings.Repeat("z", 40)+" "+newID+" refs/heads/x"),
		"no ref":             pushBody("", nil, oldID+" "+newID),
		"bad length":         "zzzz",
		"truncated":          pkt(oldID + " " + newID + " refs/heads/x\n")[:20],
		"no flush":           pkt(oldID + " " + newID + " refs/heads/x\n"),
		"push certificate":   pkt("push-cert\x00report-status\n") + "0000",
		"ref with a newline": pushBody("", nil, oldID+" "+newID+" refs/heads/a\nb"),
	} {
		if _, _, err := ReadReceivePack(strings.NewReader(body), ""); err == nil {
			t.Errorf("%s: read", name)
		}
	}
}

func FuzzReadReceivePack(f *testing.F) {
	f.Add([]byte(pushBody("report-status", nil, oldID+" "+newID+" refs/heads/legato/x")))
	f.Add([]byte("0000"))
	f.Fuzz(func(t *testing.T, data []byte) {
		push, _, err := ReadReceivePack(bytes.NewReader(data), "")
		if err == nil && len(push.Commands) == 0 {
			t.Fatal("a push with no commands was read")
		}
	})
}

func TestEachRefUpdateIsAnOperation(t *testing.T) {
	for _, tc := range []struct {
		cmd       RefUpdate
		operation string
	}{
		{RefUpdate{oldID, newID, "refs/heads/legato/x"}, OperationGitPush},
		{RefUpdate{zeroID, newID, "refs/heads/feature"}, OperationGitPush},
		{RefUpdate{oldID, newID, "refs/heads/main"}, OperationGitPushDefault},
		{RefUpdate{zeroID, newID, "refs/tags/v1"}, OperationGitPushTag},
		{RefUpdate{oldID, zeroID, "refs/heads/legato/x"}, OperationGitDeleteRef},
		{RefUpdate{oldID, zeroID, "refs/heads/main"}, OperationGitDeleteRef},
	} {
		got, err := ClassifyRefUpdate("cpave3/legato", "main", tc.cmd)
		if err != nil || got.Operation != tc.operation || got.Resource != "github:cpave3/legato@"+tc.cmd.Ref {
			t.Errorf("%+v: %+v, %v; want %s", tc.cmd, got, err, tc.operation)
		}
	}
	if _, err := ClassifyRefUpdate("cpave3/legato", "main", RefUpdate{oldID, newID, "refs/notes/commits"}); err == nil {
		t.Error("a ref outside heads and tags was classified")
	}
}

func TestARefusalIsWrittenSoGitPrintsIt(t *testing.T) {
	push := ReceivePack{Commands: []RefUpdate{{oldID, newID, "refs/heads/main"}, {oldID, newID, "refs/heads/legato/x"}}, Capabilities: []string{"report-status", "side-band-64k"}}
	out := string(RefusePush(push, map[string]string{"refs/heads/main": "default branch: deny"}, []string{"Legato: refs/heads/main is outside your grant."}))

	if !strings.Contains(out, "\x02Legato: refs/heads/main is outside your grant.\n") {
		t.Errorf("no band 2 message: %q", out)
	}
	if !strings.Contains(out, pkt("ng refs/heads/main default branch: deny\n")) || !strings.Contains(out, pkt("ng refs/heads/legato/x refused with the rest of the push\n")) {
		t.Errorf("no ng lines inside band 1: %q", out)
	}
	if !strings.HasSuffix(out, "0000") {
		t.Errorf("no final flush: %q", out)
	}

	plain := string(RefusePush(ReceivePack{Commands: push.Commands, Capabilities: []string{"report-status"}}, map[string]string{"refs/heads/main": "default branch: deny"}, []string{"Legato: x"}))
	if !strings.HasPrefix(plain, pkt("unpack ok\n")) || strings.Contains(plain, "\x02") {
		t.Errorf("without side-band: %q", plain)
	}
}
