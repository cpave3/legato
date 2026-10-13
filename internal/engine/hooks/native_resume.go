package hooks

import (
	"fmt"
	"strings"
)

func (a *ClaudeCodeAdapter) ValidateSessionLaunch(policy, tier string) error {
	flags := []string{"--resume", "-r", "--continue", "-c", "--session-id", "--fork-session"}
	if policy == "resume" {
		flags = append(flags, "--worktree", "-w", "--background", "--remote")
	}
	if err := validateNativeResumeArgs(a.launchArgs, flags); err != nil {
		return err
	}
	return validateNativeResumeArgs(a.tiers[tier], flags)
}

func (a *CodexAdapter) ValidateSessionLaunch(policy, tier string) error {
	flags := []string{"--last", "--all"}
	if policy == "resume" {
		flags = append(flags, "--cd", "-C", "--worktree", "--ephemeral", "--remote")
	}
	if err := validateNativeResumeArgs(a.launchArgs, flags); err != nil {
		return err
	}
	if err := validateNativeResumeArgs(a.tiers[tier], flags); err != nil {
		return err
	}
	args := append(append([]string(nil), a.launchArgs...), a.tiers[tier]...)
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--":
			return nil
		case "-c", "--config", "-m", "--model", "-p", "--profile", "-s", "--sandbox", "-a", "--ask-for-approval", "-i", "--image", "-C", "--cd", "--add-dir", "--enable", "--disable", "--remote", "--remote-auth-token":
			i++
		case "resume", "fork":
			return fmt.Errorf("configured subcommand %s overrides the requested conversation; remove it from launch_args/tier arguments", args[i])
		}
	}
	return nil
}

func validateNativeResumeArgs(args, flags []string) error {
	for _, arg := range args {
		for _, flag := range flags {
			if arg == flag || strings.HasPrefix(arg, flag+"=") || (len(flag) == 2 && strings.HasPrefix(arg, flag) && len(arg) > 2) {
				return fmt.Errorf("configured argument %s overrides the requested conversation or workspace; remove it from launch_args/tier arguments", arg)
			}
		}
	}
	return nil
}
