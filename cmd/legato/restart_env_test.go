package main

import (
	"os"
	"slices"
	"testing"
)

// The variables that unlock the secret stores are taken out of this
// process's environment, so agents never inherit them, but a restart in
// place gets them back.
func TestARestartInPlaceGetsBackTheSecretUnlockVariables(t *testing.T) {
	t.Setenv("LEGATO_SECRETS_PASSPHRASE", "hunter2")
	t.Setenv("OP_SERVICE_ACCOUNT_TOKEN", "ops_abc")
	launchSecretEnv = nil
	t.Cleanup(func() { launchSecretEnv = nil })

	dropSecretEnv()
	if _, ok := os.LookupEnv("LEGATO_SECRETS_PASSPHRASE"); ok {
		t.Fatal("the passphrase stayed in the environment")
	}
	env := restartEnv(os.Environ())
	for _, want := range []string{"LEGATO_SECRETS_PASSPHRASE=hunter2", "OP_SERVICE_ACCOUNT_TOKEN=ops_abc"} {
		if !slices.Contains(env, want) {
			t.Errorf("the restart's environment lacks %s", want)
		}
	}
	// A second drop (a second runtime in the process) keeps what the first saw.
	dropSecretEnv()
	if !slices.Contains(restartEnv(os.Environ()), "LEGATO_SECRETS_PASSPHRASE=hunter2") {
		t.Error("a second drop lost the passphrase")
	}
}
