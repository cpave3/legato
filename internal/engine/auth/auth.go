package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const tokenFile = "auth-token"

// EnsureToken reads the auth token from dataDir/auth-token, generating a new
// one if the file doesn't exist. Returns the hex-encoded token string.
func EnsureToken(dataDir string) (string, error) {
	path := filepath.Join(dataDir, tokenFile)
	data, err := os.ReadFile(path)
	if err == nil {
		token := strings.TrimSpace(string(data))
		if token != "" {
			return token, nil
		}
	}
	return generateToken(path)
}

// RegenerateToken generates a new random token, overwriting any existing one.
// All previously authenticated clients will need to re-authenticate.
func RegenerateToken(dataDir string) (string, error) {
	path := filepath.Join(dataDir, tokenFile)
	return generateToken(path)
}

// ReadToken reads the current token without generating one if absent.
func ReadToken(dataDir string) (string, error) {
	path := filepath.Join(dataDir, tokenFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading auth token: %w", err)
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", fmt.Errorf("auth token file is empty")
	}
	return token, nil
}

func generateToken(path string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating random token: %w", err)
	}
	token := hex.EncodeToString(b)

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", fmt.Errorf("creating token directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(token+"\n"), 0600); err != nil {
		return "", fmt.Errorf("writing auth token: %w", err)
	}
	return token, nil
}
