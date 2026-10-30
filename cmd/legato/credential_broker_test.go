package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/service"
)

// credentialHost is a host whose broker answers credential.value with a
// broker over one env:// credential that any session may read.
func credentialHost(t *testing.T) (socket, token string) {
	t.Helper()
	db, _, _, token := brokerFixture(t)
	t.Setenv("CRED_BROKER_TEST_KEY", "sk-from-host")
	values, err := service.NewCredentialBroker(db, service.NewApprovalService(db, service.ApprovalServiceOptions{}), service.CredentialBrokerOptions{
		Credentials: []config.CredentialConfig{{Name: "eleven", Secret: "env://CRED_BROKER_TEST_KEY", Profile: "secret",
			Policy: []config.CredentialRule{{Operation: []string{"read"}, Approve: "auto"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	host := startBrokerHost(context.Background(), db, shortDir(t), brokerHostOptions{Deps: cli.BrokerDeps{Ops: cli.CredentialBrokerOps(values)}})
	t.Cleanup(host.Stop)
	return host.SocketPath(), token
}

// unloadableConfig points LEGATO_CONFIG at a file that does not load, so a
// command that reads the configuration fails.
func unloadableConfig(t *testing.T) {
	t.Helper()
	bad := filepath.Join(t.TempDir(), "config.yaml")
	_ = os.WriteFile(bad, []byte("db: [not a map\n"), 0o600)
	t.Setenv("LEGATO_CONFIG", bad)
}

func TestCredentialGetAsksTheHostWithoutReadingConfigOrStore(t *testing.T) {
	socket, token := credentialHost(t)
	unloadableConfig(t)
	t.Setenv("LEGATO_BROKER_SOCKET", socket)
	t.Setenv("LEGATO_BROKER_TOKEN", token)

	var code int
	out := captureStdout(t, func() int { code = runCLI([]string{"credential", "get", "eleven"}); return code })
	if code != 0 || out != "sk-from-host" {
		t.Fatalf("exit %d, out %q", code, out)
	}
}

func TestASandboxedCredentialGetAsksTheHostDirectly(t *testing.T) {
	socket, token := credentialHost(t)
	t.Setenv("LEGATO_SANDBOXED", "1")
	t.Setenv("LEGATO_BROKER_SOCKET", socket)
	t.Setenv("LEGATO_BROKER_TOKEN", token)

	var code int
	out := captureStdout(t, func() int { code = runCLI([]string{"credential", "get", "eleven"}); return code })
	if code != 0 || out != "sk-from-host" {
		t.Fatalf("exit %d, out %q", code, out)
	}
	out = captureStdout(t, func() int {
		code = runCLI([]string{"credential", "exec", "eleven", "--env", "KEY", "--", "sh", "-c", `printf %s "$KEY"`})
		return code
	})
	if code != 0 || out != "sk-from-host" {
		t.Fatalf("exec: exit %d, out %q", code, out)
	}
}

func TestAnUnsandboxedCredentialGetReadsItselfWhenNoBrokerAnswers(t *testing.T) {
	_, dbPath, _, token := brokerFixture(t)
	t.Setenv("CRED_LOCAL_TEST_KEY", "sk-local")
	cfg := filepath.Join(t.TempDir(), "config.yaml")
	_ = os.WriteFile(cfg, []byte("db:\n  path: "+dbPath+"\ncredentials:\n  - name: eleven\n    secret: env://CRED_LOCAL_TEST_KEY\n    policy:\n      - { operation: read, approve: auto }\n"), 0o600)
	t.Setenv("LEGATO_CONFIG", cfg)
	t.Setenv("LEGATO_BROKER_SOCKET", filepath.Join(shortDir(t), "none.sock"))
	t.Setenv("LEGATO_BROKER_TOKEN", token)
	t.Setenv("LEGATO_TASK_ID", "task1")
	t.Setenv("LEGATO_EXECUTION_ID", "ex-1")

	var code int
	out := captureStdout(t, func() int { code = runCLI([]string{"credential", "get", "eleven"}); return code })
	if code != 0 || out != "sk-local" {
		t.Fatalf("exit %d, out %q", code, out)
	}
}

func TestASandboxedCredentialGetIsForwardedWholeToAHostWithoutCredentialValue(t *testing.T) {
	db, _, _, token := brokerFixture(t)
	ops := map[string]cli.BrokerOp{"cli.run": cliRunOp(fakeLegato(t), shortDir(t))}
	host := startBrokerHost(context.Background(), db, shortDir(t), brokerHostOptions{Deps: cli.BrokerDeps{Ops: ops}})
	t.Cleanup(host.Stop)
	t.Setenv("LEGATO_SANDBOXED", "1")
	t.Setenv("LEGATO_BROKER_SOCKET", host.SocketPath())
	t.Setenv("LEGATO_BROKER_TOKEN", token)

	var code int
	out := captureStdout(t, func() int { code = runCLI([]string{"credential", "get", "eleven"}); return code })
	if code != 0 || !strings.Contains(out, "args=credential get eleven") {
		t.Fatalf("exit %d, out:\n%s", code, out)
	}
}

func TestTheHostsBrokerServesCredentialValuesWhenItHasCredentials(t *testing.T) {
	db, _, _, _ := brokerFixture(t)
	cfg := &config.Config{}
	if _, ok := hostBrokerDeps(cfg, db, nil, nil).Ops["credential.value"]; ok {
		t.Fatal("credential.value without a credential broker")
	}
	credentials, err := service.NewCredentialBroker(db, service.NewApprovalService(db, service.ApprovalServiceOptions{}), service.CredentialBrokerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := hostBrokerDeps(cfg, db, nil, credentials).Ops["credential.value"]; !ok {
		t.Fatal("the host's broker does not serve credential.value")
	}
}
