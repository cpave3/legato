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
	return credentialGuidance(credentials, sources, false, service.SessionBranches{})
}

// Guidance is the credential guidance and, when stacks is true (the
// session's st can move a stack between clones; see StackSupport), how to
// build and push a stack of branches with Staccato. With stacks, an agent
// with a GitHub credential asks once for every legato/<task>- branch instead
// of one branch, so a stack is one approval.
func Guidance(credentials []service.CredentialSummary, sources []service.CredentialSourceSummary, stacks bool) string {
	return GuidanceFor(credentials, sources, stacks, service.SessionBranches{})
}

// GuidanceFor is Guidance for a session told about its branches: a
// handed-off task's names them legato/<main task id>-<a few words> instead
// of after its own task ID, and, when its handoff granted them, says that
// pushing them and opening draft pull requests from them is already
// allowed. The zero SessionBranches gives an ordinary session's text.
func GuidanceFor(credentials []service.CredentialSummary, sources []service.CredentialSourceSummary, stacks bool, branches service.SessionBranches) string {
	text := credentialGuidance(credentials, sources, stacks, branches)
	if stacks {
		text += stackGuidanceFor(branches)
	}
	return text
}

func credentialGuidance(credentials []service.CredentialSummary, sources []service.CredentialSourceSummary, stacks bool, branches service.SessionBranches) string {
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
	// A vault's secrets change while Legato runs, so they are named as the
	// vault; legato credential list names them as they are now.
	for _, vault := range VaultSecrets(sources) {
		fmt.Fprintf(&b, "- Secrets (%s): legato credential list names them. Ask for one with request_access (operation read), then use it with legato credential exec <name> --env <VARIABLE> -- <command>, which gives the value to that command only. Do not print it.\n", vault)
	}
	for _, c := range credentials {
		switch {
		case has(c.Channels, service.ChannelSSHAgent):
			continue
		case c.Source != "" && c.Git == "" && !has(c.Channels, service.ChannelHTTPProxy):
			continue
		case c.Git != "":
			b.WriteString(githubGuidance(c, stacks, branches))
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

// VaultSecrets describes each vault that gives secrets, for an agent:
// "every API credential, password and login in the 1Password vault Legato".
func VaultSecrets(sources []service.CredentialSourceSummary) []string {
	var out []string
	for _, src := range sources {
		if has(src.Profiles, "secret") {
			out = append(out, "every API credential, password and login in the 1Password vault "+strings.TrimPrefix(src.Vault, "op://"))
		}
	}
	return out
}

func has(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// githubGuidance says how to work with GitHub through Legato: reads as
// usual, a legato/ branch asked for by name, pushes as usual, and pull
// requests through Legato's API address.
func githubGuidance(c service.CredentialSummary, stacks bool, branches service.SessionBranches) string {
	repo := "<owner>/<repo>"
	for _, r := range c.Repositories {
		if !strings.Contains(r, "*") {
			repo = r
			break
		}
	}
	return fmt.Sprintf(`- %s (GitHub, for %s; owner/* is every repository of that owner): fetch and pull as usual; Legato's git credential helper gives you a read token for each of these repositories, and gh reads with one too. %s Then git push as usual: pushes go through Legato, which checks each branch. Pushing to the default branch, pushing tags and deleting branches are refused. A refused push says why in git's output; one that says pending ap-… waits for the person: carry on or end your turn, and push again after the [legato approvals] message. Use gh as usual for pull requests: gh pr create, edit, comment, review, close and reopen, and gh api calls that write to its pull requests (replies to review comments, reviews, comments), work once the person allows pr.write on the repository, and gh pr ready (and --undo) once they allow pr.ready, which Legato carries out for you. Use gh as usual for issues too: gh issue create, edit, comment, close and reopen, and gh api calls that write under repos/<owner>/<repo>/issues (comments, sub-issues, blocking), work once the person allows issue.write on the repository. Use gh for GitHub Actions too: gh run list, view, watch and download, gh workflow list and view, and gh api reads under repos/<owner>/<repo>/actions work without asking (the GitHub App needs the Actions permission); gh run rerun, cancel and delete, gh workflow run, enable and disable, and gh cache delete work once the person allows actions.write on the repository. The first such command of your session asks them and waits up to a minute. If gh says pending ap-…, carry on or end your turn, and run the command again after the [legato approvals] message. Do not ask for pr.write, issue.write or actions.write with request_access first, and do not look for another token: gh pr merge is not allowed. Never push with your own key, another remote or another token.
`, c.Name, strings.Join(c.Repositories, ", "), branchWork(c.Name, repo, stacks, branches))
}

// branchWork says which branches to work on and what to ask for before
// the first push. An ordinary session works on legato/$LEGATO_TASK_ID-…
// and asks for the one branch, or with stacks every branch of the task, so
// a stack is one approval. A handed-off session works on its main task's
// branch, and its make-a-change handoff already allowed its pushes and
// draft pull requests.
func branchWork(credential, repo string, stacks bool, branches service.SessionBranches) string {
	if branches.Base == "" {
		return "Work on a branch named legato/$LEGATO_TASK_ID-<a few words>. " + branchRequest(credential, repo, stacks, "legato/$LEGATO_TASK_ID", " (with your task ID written out)")
	}
	base := branches.Base
	work := fmt.Sprintf("Work on %s, the branch this task was handed off with, and name any new branch %s-<a few words>, with no further /.", base, base)
	allowed := base
	if given := branches.Given; given != "" {
		// Work the person began and pushed: its commits and its pull
		// request stay as they are.
		work = fmt.Sprintf("Work on %s, the branch this task was handed off with: it already has commits, so add commits on top and never rebase, amend or force-push it, and reuse its pull request. Name any new branch %s-<a few words>, with no further /.", given, base)
		allowed = given + ", " + base
	}
	if !branches.Granted {
		return work + " " + branchRequest(credential, repo, stacks, base, "")
	}
	return work + fmt.Sprintf(" Pushing %s and branches named %s-<a few words>, and opening draft pull requests from them with gh pr create --draft, are already allowed: do not ask for them with request_access. Open every pull request as a draft and never merge.", allowed, base)
}

// branchRequest says what to ask for before the first push: the one branch,
// or with stacks every branch named prefix-<a few words>.
func branchRequest(credential, repo string, stacks bool, prefix, writtenOut string) string {
	if stacks {
		return fmt.Sprintf("Before your first push, ask once for all of your branches with request_access: credential %s, operation git.push, resource github:%s@refs/heads/%s-*%s. That covers every branch named %s-<a few words>, a whole stack included, and no other.", credential, repo, prefix, writtenOut, prefix)
	}
	return fmt.Sprintf("Before your first push, ask for that one branch with request_access: credential %s, operation git.push, resource github:%s@refs/heads/legato/<the rest of your branch name>; ask for one branch, not a pattern.", credential, repo)
}
