package cli

import (
	"fmt"
	"strings"

	"github.com/cpave3/legato/internal/service"
)

// CredentialGuidance tells an agent which credentials Legato holds for it
// and how each one is used, by the channel its profile names. It follows
// MCPGuidance in the server's instructions and in the launch prompt of the
// agent tools that do not read those. "" with no credential.
func CredentialGuidance(credentials []service.CredentialSummary, sources ...service.CredentialSourceSummary) string {
	if len(credentials) == 0 && len(sources) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\nCredentials Legato holds for you. You never get their secrets, and you never look for them another way: not in files, other keys, other agents or the person's own tools. Legato asks the person when a use needs it, and a message that starts with [legato approvals] tells you what they decided.\n")
	// SSH keys share one agent, so they share one paragraph: the configured
	// ones by name, a vault's as the vault (its keys change while it runs).
	var sshNames, vaults []string
	for _, c := range credentials {
		if has(c.Channels, service.ChannelSSHAgent) && c.Source == "" {
			sshNames = append(sshNames, c.Name)
		}
	}
	for _, src := range sources {
		if has(src.Channels, service.ChannelSSHAgent) {
			vaults = append(vaults, "every SSH key in the 1Password vault "+strings.TrimPrefix(src.Vault, "op://"))
		}
	}
	if what := append(sshNames, vaults...); len(what) > 0 {
		fmt.Fprintf(&b, "- SSH keys (%s): run ssh, scp and git over ssh as usual. Your SSH_AUTH_SOCK is Legato's SSH agent, which offers these keys and signs each login the person's rules allow, for the host the server proves it is. A login to a host the person has not allowed yet waits up to a minute while they are asked, so let the command run and do not stop it. If it then fails with \"agent refused operation\" or \"Permission denied (publickey)\", they have not allowed it yet: tell them which host, carry on with other work or end your turn, and try again after the [legato approvals] message. Never get in another way: no other key or IdentityFile, no IdentityAgent or agent socket of your own, no ssh -A.\n", strings.Join(what, "; "))
	}
	for _, c := range credentials {
		switch {
		case has(c.Channels, service.ChannelSSHAgent):
			continue
		case has(c.Channels, service.ChannelHTTPProxy):
			key, base := "its key variable", "its base URL variable"
			if len(c.Env) > 0 {
				key = "$" + c.Env[0]
			}
			if len(c.Env) > 1 {
				base = "$" + c.Env[1]
			}
			fmt.Fprintf(&b, "- %s (an API key, through Legato's proxy): send its requests to %s with %s where the API takes its key. %s is a placeholder that works only through that address and only for you; Legato puts the real key in each request the rules allow. A 403 with \"status\": \"pending\" names an approval: carry on with other work or end your turn until the [legato approvals] message, then send the request again. A 403 without it is refused; do not try around it.\n", c.Name, base, key, key)
		default:
			fmt.Fprintf(&b, "- %s (a secret): ask for it with request_access, then use it with legato credential exec %s --env <VARIABLE> -- <command>, which gives the value to that command only. Do not print it.\n", c.Name, c.Name)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func has(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
