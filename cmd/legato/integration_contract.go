package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/clipboard"
)

// runIntegrationTokenContract runs `legato integration token <verb>`: the
// scoped tokens services outside Legato (Argus) use on /api/integrations/.
func runIntegrationTokenContract(verb string, args []string) int {
	command := "integration.token." + verb
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("integration", "token", verb))
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
	tokens := service.NewIntegrationTokenService(db)

	switch verb {
	case "create":
		if len(parsed.Positionals) != 1 {
			return renderCommandError(command, usageError("missing_name", "name the integration: legato integration token create <name> --scope owner:read,delegate,attention"), jsonMode)
		}
		name := parsed.Positionals[0]
		var scopes []string
		for _, scope := range strings.Split(parsed.Values["scope"], ",") {
			if scope = strings.TrimSpace(scope); scope != "" {
				scopes = append(scopes, scope)
			}
		}
		token, err := tokens.Create(ctx, name, scopes)
		if err != nil {
			return renderCommandError(command, usageError("invalid_token", err.Error()), jsonMode)
		}
		text := fmt.Sprintf("%s\n\nThe token for %s (%s). It is shown once: give it to %s now. Revoke it with: legato integration token revoke %s\n",
			token, name, strings.Join(scopes, ","), name, name)
		return writeCommandSuccess(command, map[string]any{"name": name, "token": token, "scopes": scopes}, text, jsonMode)
	case "list":
		list, err := tokens.List(ctx)
		if err != nil {
			return renderCommandError(command, &commandError{Code: "store_failed", Message: err.Error(), Exit: exitInternal}, jsonMode)
		}
		var b strings.Builder
		if len(list) == 0 {
			b.WriteString("No integration tokens. Make one with: legato integration token create <name> --scope owner:read,delegate\n")
		}
		w := tabwriter.NewWriter(&b, 0, 4, 2, ' ', 0)
		for _, t := range list {
			state := "live"
			if t.RevokedAt != nil {
				state = "revoked " + *t.RevokedAt
			}
			used := "never used"
			if t.LastUsedAt != nil {
				used = "last used " + *t.LastUsedAt
			}
			fmt.Fprintf(w, "%s\t%s\tcreated %s\t%s\t%s\n", t.Name, strings.Join(t.Scopes, ","), t.CreatedAt, used, state)
		}
		_ = w.Flush()
		return writeCommandSuccess(command, map[string]any{"tokens": list}, b.String(), jsonMode)
	case "revoke":
		if len(parsed.Positionals) != 1 {
			return renderCommandError(command, usageError("missing_name", "name the integration: legato integration token revoke <name>"), jsonMode)
		}
		if err := tokens.Revoke(ctx, parsed.Positionals[0]); err != nil {
			return renderCommandError(command, &commandError{Code: "not_found", Message: err.Error(), Exit: exitNotFound}, jsonMode)
		}
		return writeCommandSuccess(command, map[string]any{"revoked": parsed.Positionals[0]},
			fmt.Sprintf("Revoked the token of %s. It stops working at once.\n", parsed.Positionals[0]), jsonMode)
	}
	return renderCommandError(command, usageError("unknown_verb", "integration token "+verb), jsonMode)
}

// runIntegrationContract runs `legato integration connect|status|disconnect
// argus`: Legato's connection to Argus as its OAuth client, which it uses to
// send agents' replies.
func runIntegrationContract(verb string, args []string) int {
	command := "integration." + verb
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("integration", verb))
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	if len(parsed.Positionals) == 0 || parsed.Positionals[0] != "argus" {
		return renderCommandError(command, usageError("unknown_integration", "the only integration is argus: legato integration "+verb+" argus"), jsonMode)
	}
	cfg, db, code := openCommandStoreWithConfig(command, jsonMode)
	if db == nil {
		return code
	}
	defer db.Close()
	ctx := context.Background()
	secretStore := configuredSecretStore(cfg, db)
	connector := service.NewIntegrationConnector(db, service.SealedIntegrationSecrets{Store: secretStore}, nil)

	switch verb {
	case "status":
		st, err := connector.Status(ctx, "argus")
		if err != nil {
			return renderCommandError(command, &commandError{Code: "store_failed", Message: err.Error(), Exit: exitInternal}, jsonMode)
		}
		text := "Not connected to Argus. Connect with: legato integration connect argus <url>\n"
		if st.State != "none" {
			text = fmt.Sprintf("Argus at %s: %s since %s\n", st.URL, st.State, st.ConnectedAt)
			if st.State == "disconnected" {
				text += "Argus refused Legato's tokens. Connect again with: legato integration connect argus\n"
			}
		}
		text += fmt.Sprintf("Replies: %d sent, %d waiting, %d failed\n", st.Replies["sent"], st.Replies["pending"], st.Replies["failed"])
		return writeCommandSuccess(command, st, text, jsonMode)
	case "disconnect":
		if err := connector.Disconnect(ctx, "argus"); err != nil {
			return renderCommandError(command, &commandError{Code: "store_failed", Message: err.Error(), Exit: exitInternal}, jsonMode)
		}
		return writeCommandSuccess(command, map[string]any{"disconnected": "argus"}, "Legato forgot its connection to Argus. Replies wait until you connect again.\n", jsonMode)
	case "connect":
		url := ""
		if len(parsed.Positionals) > 1 {
			url = parsed.Positionals[1]
		} else if cfg.Integrations.Argus != nil {
			url = cfg.Integrations.Argus.URL
		}
		if url == "" {
			return renderCommandError(command, usageError("missing_url", "give Argus's address: legato integration connect argus <url>"), jsonMode)
		}
		// The refresh token is rewritten on every use, by a Legato nobody is
		// watching, so the master key must be readable without a person.
		if source, err := secretStore.Source(ctx); err != nil {
			return renderCommandError(command, &commandError{Code: "secrets_unavailable", Message: err.Error(), Exit: exitEnvironment}, jsonMode)
		} else if source == "passphrase" {
			return renderCommandError(command, &commandError{Code: "secrets_passphrase", Exit: exitEnvironment,
				Message: "Legato's secret store is unlocked by a passphrase, but the Argus refresh token is rewritten whenever Legato sends a reply. Set secrets.master_key to a source Legato can read alone (auto, systemd-creds, secret-service, keychain or file)"}, jsonMode)
		}
		hostname, _ := os.Hostname()
		pending, err := connector.Begin(ctx, "argus", url, "Legato ("+hostname+")")
		if err != nil {
			return renderCommandError(command, &commandError{Code: "connect_failed", Message: err.Error(), Exit: exitEnvironment}, jsonMode)
		}
		defer pending.Close()
		fmt.Fprintf(os.Stderr, "Open this address and allow Legato to reply to Argus cases:\n\n  %s\n\n", pending.URL())
		if !parsed.Present["no-browser"] {
			_ = clipboard.OpenURL(pending.URL())
		}
		fmt.Fprintln(os.Stderr, "If your browser is on another machine, it cannot reach Legato's 127.0.0.1 address: paste the address it tried here and press Enter.")
		go func() {
			scanner := bufio.NewScanner(os.Stdin)
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if line == "" {
					continue
				}
				if err := pending.Paste(line); err != nil {
					fmt.Fprintf(os.Stderr, "That address has no code: %v\n", err)
					continue
				}
				return
			}
		}()
		waitCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()
		if err := pending.Finish(waitCtx); err != nil {
			return renderCommandError(command, &commandError{Code: "connect_failed", Message: err.Error(), Exit: exitEnvironment}, jsonMode)
		}
		return writeCommandSuccess(command, map[string]any{"connected": "argus", "url": url}, "Connected to Argus. Agents' replies go to "+url+".\n", jsonMode)
	}
	return renderCommandError(command, usageError("unknown_verb", "integration "+verb), jsonMode)
}
