package main

import (
	"reflect"
	"testing"
)

func TestCodexLaunchSplitsItsOwnArgumentsFromCodexs(t *testing.T) {
	own, codex := splitCodexLaunch([]string{"task-1", "--execution-id", "ex-1", "--", "--model", "gpt-6-sol", "-c", "a=b"})
	if !reflect.DeepEqual(own, []string{"task-1", "--execution-id", "ex-1"}) || !reflect.DeepEqual(codex, []string{"--model", "gpt-6-sol", "-c", "a=b"}) {
		t.Fatalf("own = %v, codex = %v", own, codex)
	}

	// Launched with nothing of its own, and with nothing for Codex.
	own, codex = splitCodexLaunch([]string{"--", "--search"})
	if len(own) != 0 || !reflect.DeepEqual(codex, []string{"--search"}) {
		t.Fatalf("own = %v, codex = %v", own, codex)
	}
	own, codex = splitCodexLaunch([]string{"task-1"})
	if !reflect.DeepEqual(own, []string{"task-1"}) || len(codex) != 0 {
		t.Fatalf("own = %v, codex = %v", own, codex)
	}
}

func TestTheAppServerGetsTheSessionsConfigurationOverrides(t *testing.T) {
	// The terminal UI's own -c is not applied to a thread on a remote server,
	// so what configures the session has to be given to the server.
	args := []string{
		"-c", `developer_instructions=You are the conductor.`,
		"--model", "gpt-6-sol",
		"--config", "sandbox_mode=workspace-write",
		"--config=approval_policy=on-request",
		"--enable", "web_search",
		"--disable=legacy_tool",
		"--search",
		"-c",
	}

	want := []string{
		"-c", `developer_instructions=You are the conductor.`,
		"--config", "sandbox_mode=workspace-write",
		"--config=approval_policy=on-request",
		"--enable", "web_search",
		"--disable=legacy_tool",
	}
	if got := codexServerArgs(args); !reflect.DeepEqual(got, want) {
		t.Fatalf("server args = %q\nwant        %q", got, want)
	}
}

func TestTheTerminalAttachesToTheSessionsAppServer(t *testing.T) {
	got := codexAttachArgs("ws://127.0.0.1:41875", []string{"--model", "gpt-6-sol"})

	// The token travels in the environment, never on the command line.
	want := []string{"--remote", "ws://127.0.0.1:41875", "--remote-auth-token-env", "LEGATO_CODEX_REMOTE_TOKEN", "--model", "gpt-6-sol"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %q", got)
	}
}

func TestRemoteTerminalResumesTheExactSavedThread(t *testing.T) {
	_, args := splitCodexLaunch([]string{"--", "resume", "saved-thread", "--model", "selected-model", "-c", "sandbox_mode=workspace-write"})
	args = codexSessionArgs(args)
	got := codexAttachArgs("ws://127.0.0.1:41875", args)
	want := []string{"--remote", "ws://127.0.0.1:41875", "--remote-auth-token-env", "LEGATO_CODEX_REMOTE_TOKEN", "-c", "check_for_update_on_startup=false", "resume", "saved-thread", "--model", "selected-model", "-c", "sandbox_mode=workspace-write"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resume terminal args = %q, want %q", got, want)
	}
	serverArgs := codexServerArgs(args)
	if !reflect.DeepEqual(serverArgs, []string{"-c", "check_for_update_on_startup=false", "-c", "sandbox_mode=workspace-write"}) {
		t.Fatalf("resume leaked terminal arguments to app-server: %q", serverArgs)
	}
}

func TestAnAgentSessionIsNotHeldUpByCodexsUpdateNotice(t *testing.T) {
	// The notice is a menu in the terminal that nothing gets past until a
	// person answers it, which an agent started by Legato may never have.
	got := codexSessionArgs([]string{"--model", "gpt-6-sol"})
	if want := []string{"-c", "check_for_update_on_startup=false", "--model", "gpt-6-sol"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %q", got)
	}

	// Someone who asks for the check in their launch arguments gets it: a
	// later override wins.
	asked := codexSessionArgs([]string{"-c", "check_for_update_on_startup=true"})
	if want := []string{"-c", "check_for_update_on_startup=false", "-c", "check_for_update_on_startup=true"}; !reflect.DeepEqual(asked, want) {
		t.Fatalf("args = %q", asked)
	}
}

func TestOnlyACodexThatCanShareAnAppServerIsBridged(t *testing.T) {
	current := "Commands:\n  app-server        [experimental] Run the app server or related tooling\nOptions:\n      --remote <ADDR>\n          Connect the TUI to a remote app server endpoint.\n      --remote-auth-token-env <ENV_VAR>\n"
	if !codexCanBeBridged(current) {
		t.Fatal("a Codex with an app server and --remote was not bridged")
	}
	for name, help := range map[string]string{
		"no app server":         "Options:\n      --remote <ADDR>\n      --remote-auth-token-env <ENV_VAR>\n",
		"no remote terminal":    "Commands:\n  app-server   Run the app server\n",
		"no token for a remote": "Commands:\n  app-server   Run the app server\nOptions:\n      --remote <ADDR>\n",
	} {
		if codexCanBeBridged(help) {
			t.Fatalf("%s: bridged a Codex that cannot share an app server", name)
		}
	}
}
