package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/credential"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// secretInput is where `legato secret add` reads a value; tests replace it.
var secretInput io.Reader = os.Stdin

func runSecretContract(verb string, args []string) int {
	command := "secret." + verb
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("secret", verb))
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	cfg, db, code := openCommandStoreWithConfig(command, jsonMode)
	if db == nil {
		return code
	}
	defer db.Close()
	secrets := configuredSecretStore(cfg, db)
	ctx := context.Background()

	switch verb {
	case "add":
		if len(parsed.Positionals) != 1 {
			return renderCommandError(command, usageError("missing_argument", "secret add needs the secret's name"), jsonMode)
		}
		name := parsed.Positionals[0]
		var value []byte
		var err error
		if from := parsed.Values["from"]; from != "" {
			value, err = os.ReadFile(from)
		} else {
			value, err = io.ReadAll(io.LimitReader(secretInput, 1<<20))
		}
		if err != nil {
			return renderCommandError(command, &commandError{Code: "read_failed", Message: err.Error(), Exit: exitUsage}, jsonMode)
		}
		secretType := firstSet(parsed.Values["type"], credential.TypeValue)
		if err := secrets.Add(ctx, name, secretType, value); err != nil {
			return renderCommandError(command, secretCommandError(err), jsonMode)
		}
		source, _ := secrets.Source(ctx)
		return writeCommandSuccess(command, map[string]any{"name": name, "type": secretType, "master_key": source},
			fmt.Sprintf("Stored %s as legato://%s (master key: %s).\n", name, name, source), jsonMode)
	case "list":
		list, err := secrets.List(ctx)
		if err != nil {
			return renderCommandError(command, secretCommandError(err), jsonMode)
		}
		source := credential.RecordedSource(config.ResolveDataDir(cfg))
		var b strings.Builder
		if len(list) == 0 {
			b.WriteString("No secrets. Add one with: legato secret add <name> < value-file\n")
		}
		for _, s := range list {
			fmt.Fprintf(&b, "legato://%s\t%s\tupdated %s\n", s.Name, s.Type, s.UpdatedAt)
		}
		if source != "" {
			fmt.Fprintf(&b, "Master key: %s\n", source)
		}
		return writeCommandSuccess(command, map[string]any{"secrets": list, "master_key": source}, b.String(), jsonMode)
	case "remove":
		if len(parsed.Positionals) != 1 {
			return renderCommandError(command, usageError("missing_argument", "secret remove needs the secret's name"), jsonMode)
		}
		if err := secrets.Remove(ctx, parsed.Positionals[0]); err != nil {
			return renderCommandError(command, secretCommandError(err), jsonMode)
		}
		return writeCommandSuccess(command, map[string]any{"removed": parsed.Positionals[0]}, fmt.Sprintf("Removed legato://%s.\n", parsed.Positionals[0]), jsonMode)
	}
	return renderCommandError(command, usageError("unknown_command", fmt.Sprintf("unknown secret command %q", verb)), jsonMode)
}

func secretCommandError(err error) *commandError {
	switch {
	case errors.Is(err, service.ErrSecretInvalid):
		return &commandError{Code: "invalid_secret", Message: err.Error(), Exit: exitUsage}
	case errors.Is(err, credential.ErrNotFound):
		return &commandError{Code: "not_found", Message: err.Error(), Exit: exitNotFound}
	}
	return &commandError{Code: "secret_store_failed", Message: err.Error(), Exit: exitEnvironment}
}

// runCredentialGit is git's credential helper in an agent session with a
// GitHub credential: `legato credential git <credential> get|store|erase`.
// It reads neither the configuration nor the database; git runs it on
// every fetch and push.
func runCredentialGit(args []string) int {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: legato credential git <credential> get|store|erase (git runs this as its credential helper)")
		return exitUsage
	}
	session, ok := cli.GitHubSessionFromEnv(os.Getenv)
	if !ok {
		_, _ = io.Copy(io.Discard, os.Stdin)
		return exitOK
	}
	if err := cli.GitCredentialHelper(context.Background(), args[1], os.Stdin, os.Stdout, os.Stderr, session); err != nil {
		fmt.Fprintln(os.Stderr, "legato:", err)
		return exitInternal
	}
	return exitOK
}

// runCredentialGHToken prints a token for the repository a gh command acts
// on, for Legato's gh wrapper: `legato credential gh-token -- <gh args>`. A
// pull request command (gh pr edit, comment, ...) asks for pr.write, which
// the person may have to allow, and its token is printed as "<token> revoke"
// so the wrapper revokes it after gh; anything else gets a read token. It
// prints nothing, and fails, when there is no token to give.
func runCredentialGHToken(args []string) int {
	for i, a := range args {
		if a == "--" {
			args = args[i+1:]
			break
		}
	}
	session, ok := cli.GitHubSessionFromEnv(os.Getenv)
	if !ok {
		return exitEnvironment
	}
	repo, err := cli.GHRepo(args, os.Getenv, func() string {
		out, err := exec.Command("git", "remote", "get-url", "origin").Output()
		if err != nil {
			return ""
		}
		return string(out)
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "legato:", err)
		return exitUsage
	}
	operation := cli.GHOperation(args)
	token, err := session.Token(context.Background(), repo, operation)
	if err != nil {
		fmt.Fprintln(os.Stderr, "legato:", err)
		return exitForbidden
	}
	if operation == "api.read" {
		fmt.Println(token)
	} else {
		fmt.Println(token + " revoke")
	}
	return exitOK
}

func runCredentialContract(verb string, args []string) int {
	command := "credential." + verb
	// exec passes everything after -- to the command it runs.
	var commandLine []string
	if verb == "exec" {
		for i, arg := range args {
			if arg == "--" {
				args, commandLine = args[:i], args[i+1:]
				break
			}
		}
	}
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("credential", verb))
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	if verb == "exec" && sandboxed() {
		// The command runs here, in the sandbox; only the value comes from
		// the host, through the forwarded `credential get`.
		return sandboxedCredentialExec(command, args, parsed.Values["env"], commandLine, jsonMode)
	}
	cfg, db, code := openCommandStoreWithConfig(command, jsonMode)
	if db == nil {
		return code
	}
	defer db.Close()
	broker, err := configuredCredentialBroker(cfg, db, newApprovalService(cfg, db, nil, nil), nil)
	if err != nil {
		return renderCommandError(command, &commandError{Code: "config_invalid", Message: err.Error(), Exit: exitEnvironment}, jsonMode)
	}
	ctx := context.Background()

	if verb == "list" {
		// A vault's keys are listed with the configured credentials.
		refreshCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		refreshErr := broker.RefreshSources(refreshCtx)
		cancel()
		list := broker.Credentials()
		sources := broker.Sources()
		var b strings.Builder
		if len(list) == 0 && len(sources) == 0 {
			b.WriteString("No credentials are configured (credentials: or credential_sources: in config.yaml).\n")
		}
		w := tabwriter.NewWriter(&b, 0, 4, 2, ' ', 0)
		for _, c := range list {
			where := c.Secret
			if c.Source != "" {
				where = "from " + c.Source
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%d rules\n", c.Name, c.Profile, where, len(c.Rules))
		}
		_ = w.Flush()
		for _, src := range sources {
			fmt.Fprintf(&b, "Vault %s gives every SSH key in it (profile %s).\n", src.Vault, src.Profile)
		}
		if refreshErr != nil {
			fmt.Fprintf(&b, "Could not list a vault: %v\n", refreshErr)
		}
		return writeCommandSuccess(command, map[string]any{"credentials": list, "sources": sources}, b.String(), jsonMode)
	}

	if len(parsed.Positionals) != 1 {
		return renderCommandError(command, usageError("missing_argument", fmt.Sprintf("credential %s needs the credential's name", verb)), jsonMode)
	}
	name := parsed.Positionals[0]
	taskID, executionID := strings.TrimSpace(os.Getenv("LEGATO_TASK_ID")), strings.TrimSpace(os.Getenv("LEGATO_EXECUTION_ID"))
	if taskID == "" || executionID == "" {
		return renderCommandError(command, &commandError{Code: "not_in_session", Message: "credentials are given to agent sessions: LEGATO_TASK_ID and LEGATO_EXECUTION_ID are not set", Exit: exitEnvironment}, jsonMode)
	}
	resources := splitRepeated(parsed.Values["resource"])

	switch verb {
	case "request":
		operations := splitRepeated(parsed.Values["operation"])
		if len(operations) == 0 {
			return renderCommandError(command, usageError("missing_argument", "credential request needs at least one --operation"), jsonMode)
		}
		result, err := broker.Request(ctx, service.AccessRequest{TaskID: taskID, ExecutionID: executionID, Credential: name,
			Operations: operations, Resources: resources, Reason: parsed.Values["reason"], Blocking: parsed.Present["blocking"]})
		if err != nil {
			return renderCommandError(command, &commandError{Code: "request_failed", Message: err.Error(), Exit: exitUsage}, jsonMode)
		}
		text := cli.AccessText(name, operations, resources, result) + "\n"
		if result.Status != service.AccessGranted {
			if jsonMode {
				return renderCommandError(command, &commandError{Code: "access_" + result.Status, Message: text, Details: map[string]any{"approval_id": result.ApprovalID}, Exit: exitForbidden}, true)
			}
			fmt.Fprint(os.Stderr, text)
			return exitForbidden
		}
		return writeCommandSuccess(command, result, text, jsonMode)
	case "get", "exec":
		use := service.CredentialUse{TaskID: taskID, ExecutionID: executionID, Credential: name,
			Operation: firstSet(parsed.Values["operation"], "read"), Resource: firstSet(resources...)}
		secret, decision, err := broker.Value(ctx, use)
		if err != nil {
			return renderCommandError(command, &commandError{Code: "credential_unavailable", Message: err.Error(), Exit: exitEnvironment}, jsonMode)
		}
		if !decision.Allowed {
			message := cli.AccessText(name, []string{use.Operation}, nonEmptyList(use.Resource), useResult(decision))
			return renderCommandError(command, &commandError{Code: "access_refused", Message: message, Details: map[string]any{"approval_id": decision.ApprovalID}, Exit: exitForbidden}, jsonMode)
		}
		if verb == "get" {
			_, _ = os.Stdout.Write(secret.Value)
			return exitOK
		}
		variable := parsed.Values["env"]
		if variable == "" || len(commandLine) == 0 {
			return renderCommandError(command, usageError("missing_argument", "credential exec needs --env VARIABLE and a command after --"), jsonMode)
		}
		run := exec.Command(commandLine[0], commandLine[1:]...)
		run.Env = append(os.Environ(), variable+"="+string(secret.Value))
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
	return renderCommandError(command, usageError("unknown_command", fmt.Sprintf("unknown credential command %q", verb)), jsonMode)
}

// useResult says what a refused use came to, as a request's result.
func useResult(d service.UseDecision) service.AccessResult {
	if d.ApprovalID != "" && d.Reason == "waiting for the person" {
		return service.AccessResult{Status: service.AccessPending, ApprovalID: d.ApprovalID}
	}
	return service.AccessResult{Status: service.AccessDenied, ApprovalID: d.ApprovalID, Reason: d.Reason}
}

func runAuditContract(verb string, args []string) int {
	command := "audit." + verb
	path := []string{"audit"}
	if verb == "verify" {
		path = append(path, "verify")
	}
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs(path...))
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	_, db, code := openCommandStoreWithConfig(command, jsonMode)
	if db == nil {
		return code
	}
	defer db.Close()
	ctx := context.Background()
	if verb == "verify" {
		bad, err := db.VerifyCredentialAudit(ctx)
		if err != nil {
			return renderCommandError(command, &commandError{Code: "internal_error", Message: err.Error(), Exit: exitInternal}, jsonMode)
		}
		if bad != 0 {
			return renderCommandError(command, &commandError{Code: "chain_broken", Message: fmt.Sprintf("the credential audit log was changed at row %d", bad), Details: map[string]any{"row": bad}, Exit: exitForbidden}, jsonMode)
		}
		return writeCommandSuccess(command, map[string]any{"valid": true}, "The credential audit log is intact.\n", jsonMode)
	}
	limit := 50
	if v := parsed.Values["limit"]; v != "" {
		if _, err := fmt.Sscan(v, &limit); err != nil || limit <= 0 {
			return renderCommandError(command, usageError("invalid_flag_value", "--limit takes a positive number"), jsonMode)
		}
	}
	entries, err := db.ListCredentialAudit(ctx, store.CredentialAuditFilter{Credential: parsed.Values["credential"], TaskID: parsed.Values["task"], ExecutionID: parsed.Values["execution"], Limit: limit})
	if err != nil {
		return renderCommandError(command, &commandError{Code: "internal_error", Message: err.Error(), Exit: exitInternal}, jsonMode)
	}
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 4, 2, ' ', 0)
	for _, e := range entries {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", e.At, e.Event, e.Credential, e.Operation, e.Resource, e.TaskID, firstSet(e.ApprovalID, e.Detail))
	}
	_ = w.Flush()
	if len(entries) == 0 {
		b.WriteString("Nothing recorded.\n")
	}
	return writeCommandSuccess(command, map[string]any{"entries": entries}, b.String(), jsonMode)
}

func splitRepeated(value string) []string {
	if value == "" {
		return nil
	}
	return strings.Split(value, "\x00")
}

func nonEmptyList(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}

func firstSet(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
