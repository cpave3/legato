package service

import (
	"os"
	"path/filepath"

	"github.com/cpave3/legato/config"
)

// AzureProfile is the profile of a credential az signs in with: an Entra
// app's client secret, its settings the app's client ID and its tenant.
const AzureProfile = "azure"

// SandboxAzure is the credential a sandbox's az signs in with: in the
// sandbox, az is Legato's wrapper, which signs in as the app before the
// first command that needs an account.
type SandboxAzure struct {
	Credential, ClientID, TenantID string
}

// SandboxAzureFor is the first credential of the azure profile, which a
// sandbox's az signs in with; the zero value when there is none.
func SandboxAzureFor(credentials []config.CredentialConfig) SandboxAzure {
	for _, c := range credentials {
		if c.Profile == AzureProfile {
			return SandboxAzure{Credential: c.Name, ClientID: c.Settings.ClientID, TenantID: c.Settings.TenantID}
		}
	}
	return SandboxAzure{}
}

// writeAzWrapper writes the az wrapper at path. It runs the real az, at
// LEGATO_AZ_REAL, and first signs in as the app when the session's az
// configuration holds no account: through legato credential exec, so the
// credential's rules apply and the secret reaches the az login alone. Signing
// in and out, the version and help are left to az. It is only ever placed in
// a sandbox, whose home (and so ~/.azure) is the session's own.
func writeAzWrapper(path, legato string, az SandboxAzure) error {
	script := "#!/bin/sh\n" +
		"# Legato: az, signed in first as the app of the credential " + az.Credential + ".\n" +
		"real=${LEGATO_AZ_REAL:-}\n" +
		// The real az's own path is this wrapper: without LEGATO_AZ_REAL
		// it would run itself for ever.
		"if [ -z \"$real\" ] || [ \"$real\" -ef \"$0\" ]; then\n" +
		"  echo \"legato: this az is Legato's wrapper and the real az is not reachable: LEGATO_AZ_REAL is not set.\" >&2\n" +
		"  exit 127\n" +
		"fi\n" +
		"case ${1-} in ''|login|logout|version|--version|help|-h|--help) exec \"$real\" \"$@\" ;; esac\n" +
		"for arg do case $arg in -h|--help) exec \"$real\" \"$@\" ;; esac; done\n" +
		// az writes an account's "user" into azureProfile.json; reading it
		// is cheaper than az account show.
		"if ! grep -q '\"user\"' \"${AZURE_CONFIG_DIR:-$HOME/.azure}/azureProfile.json\" 2>/dev/null; then\n" +
		"  " + shellQuote(legato) + " credential exec " + shellQuote(az.Credential) + " --env AZURE_CLIENT_SECRET -- " +
		"/bin/sh -c 'exec \"$0\" login --service-principal --username \"$1\" --tenant \"$2\" --password=\"$AZURE_CLIENT_SECRET\" --allow-no-subscriptions --output none' " +
		"\"$real\" " + shellQuote(az.ClientID) + " " + shellQuote(az.TenantID) + "\n" +
		"  status=$?\n" +
		"  if [ $status -eq 4 ]; then\n" +
		"    echo \"legato: az is not signed in: Legato did not give the credential " + az.Credential + " (above). If the person is being asked, you get a [legato approvals] message when they decide; run this az command again once it is approved.\" >&2\n" +
		"    exit 4\n" +
		"  elif [ $status -ne 0 ]; then\n" +
		"    echo \"legato: az could not sign in as the app of the credential " + az.Credential + " (above).\" >&2\n" +
		"    exit $status\n" +
		"  fi\n" +
		"fi\n" +
		"exec \"$real\" \"$@\"\n"
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeIfChanged(path, script, 0o700)
}
