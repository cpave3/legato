package service

import "strings"

const legatoDetailsHeading = "## Legato details"

// effectiveDescriptionMD combines provider-owned Markdown with the task's
// Legato-owned Markdown for display and agent context.
func effectiveDescriptionMD(provider, remoteDescriptionMD, localDescriptionMD string) string {
	if provider == "" {
		return localDescriptionMD
	}
	remote := strings.TrimSpace(remoteDescriptionMD)
	local := strings.TrimSpace(localDescriptionMD)
	if local == "" {
		return remote
	}
	legato := legatoDetailsHeading + "\n\n" + local
	if remote == "" {
		return legato
	}
	return remote + "\n\n" + legato
}

func taskDescriptionMD(taskProvider *string, remoteDescriptionMD, localDescriptionMD string) string {
	provider := ""
	if taskProvider != nil {
		provider = *taskProvider
	}
	return effectiveDescriptionMD(provider, remoteDescriptionMD, localDescriptionMD)
}
