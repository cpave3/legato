package pairing

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/sha256"
	"errors"
	"io"
)

// What each side seals is sealed under a key of its own direction.
const (
	LabelPhone  = "legato pair phone"
	LabelServer = "legato pair server"
)

// Protocol is the SPAKE2 configuration of Legato's pairing: the phone is A.
var Protocol = Config{
	IdentityA: []byte("legato-mobile"),
	IdentityB: []byte("legato-server"),
	AAD:       []byte("legato-pair-v1"),
}

func aead(key []byte, label string) (cipher.AEAD, error) {
	k, err := hkdf.Key(sha256.New, key, nil, label, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(k)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Seal encrypts plaintext under a key derived from the exchange's key and the
// label: AES-256-GCM, a random 12-byte nonce first.
func Seal(key []byte, label string, plaintext []byte, random io.Reader) ([]byte, error) {
	gcm, err := aead(key, label)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(random, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// Open decrypts what Seal made.
func Open(key []byte, label string, sealed []byte) ([]byte, error) {
	gcm, err := aead(key, label)
	if err != nil {
		return nil, err
	}
	if len(sealed) < gcm.NonceSize()+gcm.Overhead() {
		return nil, errors.New("pairing: sealed text is too short")
	}
	n := gcm.NonceSize()
	out, err := gcm.Open(nil, sealed[:n], sealed[n:], nil)
	if err != nil {
		return nil, errors.New("pairing: sealed text does not open")
	}
	return out, nil
}
