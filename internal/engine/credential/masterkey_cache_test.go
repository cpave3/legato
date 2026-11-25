package credential

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// count says how many times the runner ran a command such as
// "systemd-creds decrypt".
func (r *scriptedRunner) count(command string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, call := range r.calls {
		if call == command {
			n++
		}
	}
	return n
}

func (r *scriptedRunner) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = nil
}

// enrolledWithSystemdCreds makes a key with systemd-creds, then forgets it,
// as a new process would start.
func enrolledWithSystemdCreds(t *testing.T) (*scriptedRunner, MasterKeyOptions, []byte) {
	t.Helper()
	runner := newScriptedRunner()
	opts := options(t, runner, "linux", "systemd-creds")
	key, source, err := LoadMasterKey(context.Background(), opts)
	if err != nil || source != SourceSystemdCreds {
		t.Fatalf("source %q err %v", source, err)
	}
	ForgetMasterKey(opts.DataDir)
	runner.reset()
	return runner, opts, key
}

func TestTheMasterKeyIsUnsealedOncePerProcess(t *testing.T) {
	runner, opts, key := enrolledWithSystemdCreds(t)

	for i := 0; i < 10; i++ {
		got, source, err := LoadMasterKey(context.Background(), opts)
		if err != nil || source != SourceSystemdCreds || !bytes.Equal(got, key) {
			t.Fatalf("load %d: source %q err %v same=%v", i, source, err, bytes.Equal(got, key))
		}
	}
	if n := runner.count("systemd-creds decrypt"); n != 1 {
		t.Fatalf("10 loads unsealed the key %d times, want 1", n)
	}
}

func TestACallerCannotChangeTheCachedKey(t *testing.T) {
	_, opts, key := enrolledWithSystemdCreds(t)

	got, _, err := LoadMasterKey(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	clear(got)
	again, _, err := LoadMasterKey(context.Background(), opts)
	if err != nil || !bytes.Equal(again, key) {
		t.Fatalf("the cached key changed with a caller's copy: %v", err)
	}
}

func TestAKeyMadeAgainAfterTheStoreWasResetIsUsed(t *testing.T) {
	runner, opts, key := enrolledWithSystemdCreds(t)
	if _, _, err := LoadMasterKey(context.Background(), opts); err != nil {
		t.Fatal(err)
	}

	// The operator starts the store again.
	for _, name := range []string{"secrets.source", "secrets.key.cred"} {
		if err := os.Remove(filepath.Join(opts.DataDir, name)); err != nil {
			t.Fatal(err)
		}
	}
	runner.mu.Lock()
	delete(runner.stored, "systemd-creds")
	runner.mu.Unlock()
	runner.reset()

	fresh, _, err := LoadMasterKey(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(fresh, key) {
		t.Fatal("the old cached key was given after the store was made again")
	}
	if runner.count("systemd-creds encrypt") != 1 {
		t.Fatalf("no new key was made: calls %v", runner.calls)
	}
	runner.reset()
	again, _, err := LoadMasterKey(context.Background(), opts)
	if err != nil || !bytes.Equal(again, fresh) || runner.count("systemd-creds decrypt") != 0 {
		t.Fatalf("the new key was not cached: err %v same=%v calls %v", err, bytes.Equal(again, fresh), runner.calls)
	}
}

func TestAKeyAnotherProcessEnrolledIsUsed(t *testing.T) {
	runner, opts, key := enrolledWithSystemdCreds(t)
	if _, _, err := LoadMasterKey(context.Background(), opts); err != nil {
		t.Fatal(err)
	}

	// Another process enrolled a new key while this one held the old.
	other, err := newKey()
	if err != nil {
		t.Fatal(err)
	}
	nonce, check, err := Seal(other, "check", TypeValue, []byte(checkText))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(keyState{Source: SourceSystemdCreds, Nonce: nonce, Check: check})
	if err := os.WriteFile(filepath.Join(opts.DataDir, "secrets.source"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	runner.mu.Lock()
	runner.stored["systemd-creds"] = []byte(hex.EncodeToString(other))
	runner.mu.Unlock()
	runner.reset()

	got, _, err := LoadMasterKey(context.Background(), opts)
	if err != nil || bytes.Equal(got, key) || !bytes.Equal(got, other) {
		t.Fatalf("err %v; old key=%v, new key=%v", err, bytes.Equal(got, key), bytes.Equal(got, other))
	}
	if runner.count("systemd-creds decrypt") != 1 {
		t.Fatalf("the new key was not unsealed: calls %v", runner.calls)
	}
}

func TestAFailedLoadIsNotCached(t *testing.T) {
	runner, opts, key := enrolledWithSystemdCreds(t)

	runner.mu.Lock()
	runner.fail["systemd-creds"] = true
	runner.mu.Unlock()
	if _, _, err := LoadMasterKey(context.Background(), opts); err == nil {
		t.Fatal("a failed unseal gave a key")
	}
	runner.mu.Lock()
	runner.fail["systemd-creds"] = false
	runner.mu.Unlock()
	got, _, err := LoadMasterKey(context.Background(), opts)
	if err != nil || !bytes.Equal(got, key) {
		t.Fatalf("after the unseal works again: err %v", err)
	}
}

func TestConcurrentFirstLoadsUnsealOnce(t *testing.T) {
	runner, opts, key := enrolledWithSystemdCreds(t)

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, _, err := LoadMasterKey(context.Background(), opts)
			if err == nil && !bytes.Equal(got, key) {
				err = errors.New("another key")
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if n := runner.count("systemd-creds decrypt"); n != 1 {
		t.Fatalf("8 concurrent loads unsealed the key %d times, want 1", n)
	}
}

func TestForgettingTheKeyMakesTheNextLoadUnsealIt(t *testing.T) {
	runner, opts, _ := enrolledWithSystemdCreds(t)
	if _, _, err := LoadMasterKey(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	ForgetMasterKey(opts.DataDir)
	if _, _, err := LoadMasterKey(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if n := runner.count("systemd-creds decrypt"); n != 2 {
		t.Fatalf("decrypts = %d, want 2 (one before and one after forgetting)", n)
	}
}

func TestAWrongMasterKeyIsNamedAsSuch(t *testing.T) {
	key, _ := newKey()
	other, _ := newKey()
	records := memoryRecords{}
	if err := (LegatoStore{Records: records, MasterKey: key}).Put(context.Background(), Ref{Scheme: "legato", Path: "x"}, Secret{Type: TypeValue, Value: []byte("v")}); err != nil {
		t.Fatal(err)
	}
	_, err := LegatoStore{Records: records, MasterKey: other}.Get(context.Background(), Ref{Scheme: "legato", Path: "x"})
	if !errors.Is(err, ErrWrongMasterKey) {
		t.Fatalf("err = %v, want ErrWrongMasterKey", err)
	}
}
