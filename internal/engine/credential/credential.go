// Package credential holds the parts of Legato's credential broker that know
// nothing about Legato: secret types, secret references and the stores they
// name, sealing values at rest, master keys, resource matching, and the
// interfaces of the plug-in kinds (stores, issuers, channels, inspectors).
package credential

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// Secret types. Every plug-in says which it accepts.
const (
	TypeValue        = "value"
	TypeUserPass     = "userpass"
	TypeKeyPair      = "keypair"
	TypeOAuthClient  = "oauth-client"
	TypeOAuthRefresh = "oauth-refresh"
)

// KnownType reports whether t is a secret type.
func KnownType(t string) bool {
	switch t {
	case TypeValue, TypeUserPass, TypeKeyPair, TypeOAuthClient, TypeOAuthRefresh:
		return true
	}
	return false
}

// Secret is a value read from a store. It is never logged or put in an error.
type Secret struct {
	Type  string
	Value []byte
}

// String hides the value, so a Secret printed by mistake shows nothing.
func (s Secret) String() string { return "credential.Secret{" + s.Type + ", …}" }

// ErrNotFound: the store has no secret by that reference.
var ErrNotFound = errors.New("secret not found")

// Store reads secrets by reference.
type Store interface {
	Get(ctx context.Context, ref Ref) (Secret, error)
}

// Putter is a store that can keep a secret.
type Putter interface {
	Put(ctx context.Context, ref Ref, secret Secret) error
}

// Deleter is a store that can forget a secret.
type Deleter interface {
	Delete(ctx context.Context, ref Ref) error
}

// Signer is a store that signs with a key it does not give out.
type Signer interface {
	Sign(ctx context.Context, ref Ref, data []byte, algorithm string) ([]byte, error)
}

// Ref names a secret in a store: <scheme>://<path>.
type Ref struct {
	Scheme string
	Path   string
}

func (r Ref) String() string { return r.Scheme + "://" + r.Path }

// Schemes are the stores a reference can name.
var Schemes = []string{"legato", "op", "systemd-creds", "keychain", "env", "file"}

var (
	envName    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	secretName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
)

// ValidSecretName reports whether name can name a secret in Legato's own store.
func ValidSecretName(name string) bool { return secretName.MatchString(name) }

// ParseRef reads a secret reference and checks its path suits its store.
func ParseRef(s string) (Ref, error) {
	scheme, path, ok := strings.Cut(strings.TrimSpace(s), "://")
	if !ok || path == "" {
		return Ref{}, fmt.Errorf("secret reference %q: want <store>://<path>, such as legato://name or op://vault/item/field", s)
	}
	ref := Ref{Scheme: scheme, Path: path}
	switch scheme {
	case "legato", "systemd-creds":
		if !secretName.MatchString(path) {
			return Ref{}, fmt.Errorf("secret reference %q: %q is not a secret name", s, path)
		}
	case "op":
		if strings.Count(path, "/") < 2 {
			return Ref{}, fmt.Errorf("secret reference %q: want op://vault/item/field", s)
		}
	case "keychain":
		service, account, ok := strings.Cut(path, "/")
		if !ok || service == "" || account == "" {
			return Ref{}, fmt.Errorf("secret reference %q: want keychain://service/account", s)
		}
	case "env":
		if !envName.MatchString(path) {
			return Ref{}, fmt.Errorf("secret reference %q: %q is not an environment variable name", s, path)
		}
	case "file":
		if !strings.HasPrefix(path, "/") {
			return Ref{}, fmt.Errorf("secret reference %q: a file reference needs an absolute path, file:///path", s)
		}
	default:
		return Ref{}, fmt.Errorf("secret reference %q: unknown store %q (known: %s)", s, scheme, strings.Join(Schemes, ", "))
	}
	return ref, nil
}

// Covers reports whether a rule's resource pattern covers a resource. An empty
// pattern covers everything. A resource that is itself a pattern is covered
// when the rule's pattern matches it as a string, so a rule for
// refs/heads/legato/** covers a request for refs/heads/legato/fix-*.
func Covers(pattern, resource string) bool {
	if pattern == "" {
		return true
	}
	ok, err := doublestar.Match(pattern, resource)
	return err == nil && ok
}

// Specificity is how much of a pattern is literal: the more, the more specific.
func Specificity(pattern string) int {
	n := 0
	for _, r := range pattern {
		switch r {
		case '*', '?', '[', ']', '{', '}':
		default:
			n++
		}
	}
	return n
}

// IsPattern reports whether a resource is a pattern rather than one resource.
func IsPattern(resource string) bool {
	return strings.ContainsAny(resource, "*?[{")
}
