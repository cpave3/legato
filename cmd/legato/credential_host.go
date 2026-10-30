package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"time"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/broker"
)

// credentialHostTimeout bounds asking the host for a value: a use no grant
// covers waits up to a minute for the person, and the host's first read
// from 1Password signs in, which takes a few seconds.
const credentialHostTimeout = 2 * time.Minute

// credentialAnswer is what asking the host came to.
type credentialAnswer int

const (
	// askedNobody: no broker, or none answered outside the sandbox. The
	// command reads the value itself, as before.
	askedNobody credentialAnswer = iota
	// hostAnswered: the host decided, with the result or an error.
	hostAnswered
	// hostTooOld: a sandboxed session's host has no credential.value; the
	// command is forwarded whole, as before.
	hostTooOld
)

// credentialFromHost asks the host Legato for a credential's value for this
// session. The host reads it with the broker it keeps, so its stores (such
// as 1Password) are signed in once, and decides who is asking from the
// session's broker token rather than from LEGATO_TASK_ID.
func credentialFromHost(name, operation, resource string) (cli.CredentialValueResult, credentialAnswer, error) {
	socket, token, ok := brokerSession()
	if !ok {
		return cli.CredentialValueResult{}, askedNobody, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), credentialHostTimeout)
	defer cancel()
	var result cli.CredentialValueResult
	err := broker.Call(ctx, socket, token, "credential.value", map[string]string{"credential": name, "operation": operation, "resource": resource}, &result)
	var refused *broker.Error
	switch {
	case err == nil:
		return result, hostAnswered, nil
	case errors.As(err, &refused) && refused.Code == "unknown_op":
		if sandboxed() {
			return result, hostTooOld, nil
		}
		return result, askedNobody, nil
	case errors.Is(err, broker.ErrUnreachable) && !sandboxed():
		return result, askedNobody, nil
	}
	return result, hostAnswered, err
}

// deliverCredential prints a value (get) or runs a command with it in one
// variable (exec).
func deliverCredential(command, verb string, value []byte, variable string, commandLine []string, jsonMode bool) int {
	if verb == "get" {
		_, _ = os.Stdout.Write(value)
		return exitOK
	}
	if variable == "" || len(commandLine) == 0 {
		return renderCommandError(command, usageError("missing_argument", "credential exec needs --env VARIABLE and a command after --"), jsonMode)
	}
	run := exec.Command(commandLine[0], commandLine[1:]...)
	run.Env = append(os.Environ(), variable+"="+string(value))
	run.Stdin, run.Stdout, run.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := run.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return exit.ExitCode()
		}
		return renderCommandError(command, &commandError{Code: "exec_failed", Message: err.Error(), Exit: exitDependency}, jsonMode)
	}
	return exitOK
}
