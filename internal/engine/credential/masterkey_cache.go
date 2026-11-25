package credential

import (
	"crypto/sha256"
	"errors"
	"path/filepath"
	"sync"
)

// ErrWrongMasterKey: a sealed value does not open with the master key it
// was read with.
var ErrWrongMasterKey = errors.New("cannot be opened with this master key")

// The master key is unsealed once per process and kept here, in memory
// only, because an unseal can be slow: systemd-creds on a TPM takes about a
// second, secret-tool may ask to unlock the keyring, a passphrase takes
// 600,000 PBKDF2 rounds. LoadMasterKey checks a cached key against
// secrets.source on every use, so a key enrolled again (here or by another
// process) is unsealed afresh. keyMu is held across a whole load, so
// callers starting at once unseal once.
var (
	keyMu    sync.Mutex
	keyCache = map[keyCacheID]cachedKey{}
)

// keyCacheID: a data folder and the passphrase given, so a wrong
// passphrase never finds the key a right one unsealed.
type keyCacheID struct {
	dataDir    string
	passphrase [sha256.Size]byte
}

type cachedKey struct {
	source string
	key    []byte
}

func cacheID(opts MasterKeyOptions) keyCacheID {
	dir, err := filepath.Abs(opts.DataDir)
	if err != nil {
		dir = filepath.Clean(opts.DataDir)
	}
	return keyCacheID{dataDir: dir, passphrase: sha256.Sum256([]byte(opts.Passphrase))}
}

// cachedMasterKey returns a copy of the key cached for opts when it was
// unsealed from source and still opens state's check value. A key that no
// longer does is dropped. keyMu must be held.
func cachedMasterKey(opts MasterKeyOptions, state keyState) ([]byte, bool) {
	id := cacheID(opts)
	entry, ok := keyCache[id]
	if !ok {
		return nil, false
	}
	if entry.source != state.Source || !opensCheck(entry.key, state) {
		forget(id)
		return nil, false
	}
	return append([]byte(nil), entry.key...), true
}

// cacheMasterKey keeps a copy of a key that was just unsealed or made.
// keyMu must be held.
func cacheMasterKey(opts MasterKeyOptions, source string, key []byte) {
	id := cacheID(opts)
	forget(id)
	keyCache[id] = cachedKey{source: source, key: append([]byte(nil), key...)}
}

func opensCheck(key []byte, state keyState) bool {
	plain, err := Open(key, "check", TypeValue, state.Nonce, state.Check)
	return err == nil && string(plain) == checkText
}

// ForgetMasterKey drops the keys cached for a data folder, so the next load
// unseals the key again. The cached bytes are zeroed; copies already handed
// out, or moved by the garbage collector, are not, which is why there is no
// zeroing at shutdown either.
func ForgetMasterKey(dataDir string) {
	keyMu.Lock()
	defer keyMu.Unlock()
	dir := cacheID(MasterKeyOptions{DataDir: dataDir}).dataDir
	for id := range keyCache {
		if id.dataDir == dir {
			forget(id)
		}
	}
}

func forget(id keyCacheID) {
	if entry, ok := keyCache[id]; ok {
		clear(entry.key)
		delete(keyCache, id)
	}
}
