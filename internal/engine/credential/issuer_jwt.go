package credential

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ExchangeTemplate is the program of the jwt-exchange issuer, as a profile
// writes it: sign a JWT with the credential's key, optionally look a value
// up, exchange the JWT for a token, and revoke the token. Placeholders are
// replaced with plain values; no template can run code or reach a host the
// profile does not name.
type ExchangeTemplate struct {
	Headers  map[string]string `yaml:"headers"`
	JWT      JWTTemplate       `yaml:"jwt"`
	Lookup   *TemplateRequest  `yaml:"lookup"`
	Exchange TemplateRequest   `yaml:"exchange"`
	Revoke   *TemplateRequest  `yaml:"revoke"`
}

// JWTTemplate is the JWT the issuer signs.
type JWTTemplate struct {
	Alg    string         `yaml:"alg"`
	Claims map[string]any `yaml:"claims"`
}

// TemplateRequest is one HTTPS request of a template.
type TemplateRequest struct {
	Method string `yaml:"method"`
	URL    string `yaml:"url"`
	// Auth is jwt (Bearer the JWT), issued (Bearer the issued token) or none.
	Auth string `yaml:"auth"`
	Body any    `yaml:"body"`
	// Set names values read from the response by JSON path ($.id), for the
	// requests after it (lookup only).
	Set map[string]string `yaml:"set"`
	// Token and ExpiresAt are where the response holds them (exchange only).
	Token     string `yaml:"token"`
	ExpiresAt string `yaml:"expires_at"`
}

var placeholder = regexp.MustCompile(`\{([^{}]+)\}`)
var nowPlaceholder = regexp.MustCompile(`^now([+-]\d+)?$`)

// grantPlaceholders are the values a grant gives a template.
var grantPlaceholders = map[string]bool{"owner": true, "repo": true, "permissions": true}

// Validate checks a template as a profile gives it: every URL is https with
// a fixed host, every placeholder is one the issuer fills, and the exchange
// says where its token is.
func (t ExchangeTemplate) Validate() error {
	known := map[string]bool{}
	for k := range grantPlaceholders {
		known[k] = true
	}
	if t.Lookup != nil {
		for name := range t.Lookup.Set {
			known[name] = true
		}
	}
	if t.JWT.Alg != "" && t.JWT.Alg != "RS256" {
		return fmt.Errorf("jwt.alg %q: only RS256", t.JWT.Alg)
	}
	check := func(where string, r *TemplateRequest) error {
		if r == nil {
			return nil
		}
		if !strings.HasPrefix(r.URL, "https://") {
			return fmt.Errorf("%s.url %q: want an https address", where, r.URL)
		}
		host, _, _ := strings.Cut(strings.TrimPrefix(r.URL, "https://"), "/")
		if host == "" || strings.ContainsAny(host, "{}") {
			return fmt.Errorf("%s.url %q: the host must be written out, not filled in", where, r.URL)
		}
		switch r.Auth {
		case "", "jwt", "issued", "none":
		default:
			return fmt.Errorf("%s.auth %q: want jwt, issued or none", where, r.Auth)
		}
		for _, name := range placeholdersIn(r.URL, r.Body) {
			if !knownPlaceholder(name, known) {
				return fmt.Errorf("%s: unknown placeholder {%s}", where, name)
			}
		}
		return nil
	}
	if err := check("lookup", t.Lookup); err != nil {
		return err
	}
	if t.Lookup != nil && t.Lookup.Auth == "issued" {
		return errors.New("lookup.auth: a lookup comes before any token is issued")
	}
	if err := check("exchange", &t.Exchange); err != nil {
		return err
	}
	if t.Exchange.Token == "" {
		return errors.New("exchange.token: say where the response holds the token ($.token)")
	}
	if err := check("revoke", t.Revoke); err != nil {
		return err
	}
	for _, name := range placeholdersIn("", t.JWT.Claims) {
		if !knownPlaceholder(name, known) {
			return fmt.Errorf("jwt.claims: unknown placeholder {%s}", name)
		}
	}
	return nil
}

func knownPlaceholder(name string, known map[string]bool) bool {
	return known[name] || strings.HasPrefix(name, "settings.") || nowPlaceholder.MatchString(name)
}

func placeholdersIn(values ...any) []string {
	var out []string
	var walk func(v any)
	walk = func(v any) {
		switch v := v.(type) {
		case string:
			for _, m := range placeholder.FindAllStringSubmatch(v, -1) {
				out = append(out, m[1])
			}
		case map[string]any:
			for _, x := range v {
				walk(x)
			}
		case []any:
			for _, x := range v {
				walk(x)
			}
		}
	}
	for _, v := range values {
		walk(v)
	}
	return out
}

// fill replaces placeholders in a template value. A string that is one
// placeholder becomes the value itself (a number, an object); otherwise
// values are written into the string.
func fill(v any, values func(string) (any, bool)) (any, error) {
	switch v := v.(type) {
	case string:
		if m := placeholder.FindStringSubmatch(v); m != nil && m[0] == v {
			value, ok := values(m[1])
			if !ok {
				return nil, fmt.Errorf("no value for {%s}", m[1])
			}
			return value, nil
		}
		var missing string
		out := placeholder.ReplaceAllStringFunc(v, func(s string) string {
			value, ok := values(s[1 : len(s)-1])
			if !ok {
				missing = s
				return s
			}
			return fmt.Sprint(value)
		})
		if missing != "" {
			return nil, fmt.Errorf("no value for %s", missing)
		}
		return out, nil
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, x := range v {
			filled, err := fill(x, values)
			if err != nil {
				return nil, err
			}
			out[k] = filled
		}
		return out, nil
	case []any:
		out := make([]any, len(v))
		for i, x := range v {
			filled, err := fill(x, values)
			if err != nil {
				return nil, err
			}
			out[i] = filled
		}
		return out, nil
	}
	return v, nil
}

// minTokenLife is how long a cached token must still last to be reused.
const minTokenLife = 5 * time.Minute

// lookupLife is how long a looked-up value (an installation) is kept.
const lookupLife = time.Hour

// JWTExchange issues tokens by the jwt-exchange template: for GitHub, App
// installation tokens narrowed to one repository and the permissions an
// operation needs. Tokens are cached while they last and their scope
// matches; a revoked token is never handed out again.
type JWTExchange struct {
	Template ExchangeTemplate
	Settings map[string]string
	Client   *http.Client
	Now      func() time.Time

	mu      sync.Mutex
	tokens  map[string]Issued // by scope
	scopes  map[string]string // token ID -> scope key
	lookups map[string]lookedUp
}

type lookedUp struct {
	values map[string]any
	at     time.Time
}

func (j *JWTExchange) Accepts() []string { return []string{TypeKeyPair} }

func (j *JWTExchange) now() time.Time {
	if j.Now != nil {
		return j.Now()
	}
	return time.Now()
}

func (j *JWTExchange) client() *http.Client {
	if j.Client != nil {
		return j.Client
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// Cached returns a token already made for a scope that still lasts, so a
// caller need not read the signing key to reuse it.
func (j *JWTExchange) Cached(scope Scope) (Issued, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	cached, ok := j.tokens[scopeCacheKey(scope.Values)]
	if !ok || cached.ExpiresAt.Sub(j.now()) <= minTokenLife {
		return Issued{}, false
	}
	return cached, true
}

// Issue makes (or reuses) a token for scope.Values: owner, repo and
// permissions.
func (j *JWTExchange) Issue(ctx context.Context, secret Secret, scope Scope) (Issued, error) {
	scopeKey := scopeCacheKey(scope.Values)
	j.mu.Lock()
	if cached, ok := j.tokens[scopeKey]; ok && cached.ExpiresAt.Sub(j.now()) > minTokenLife {
		j.mu.Unlock()
		return cached, nil
	}
	j.mu.Unlock()

	key, err := rsaKey(secret.Value)
	if err != nil {
		return Issued{}, err
	}
	values := map[string]any{}
	for k, v := range scope.Values {
		values[k] = v
	}
	if j.Template.Lookup != nil {
		found, err := j.lookup(ctx, key, values)
		if err != nil {
			return Issued{}, err
		}
		for k, v := range found {
			values[k] = v
		}
	}
	jwt, err := j.signJWT(key, values)
	if err != nil {
		return Issued{}, err
	}
	resp, err := j.do(ctx, j.Template.Exchange, values, jwt, "")
	if err != nil {
		return Issued{}, fmt.Errorf("getting a token: %w", err)
	}
	token, _ := jsonAt(resp, j.Template.Exchange.Token).(string)
	if token == "" {
		return Issued{}, errors.New("getting a token: the response holds none")
	}
	issued := Issued{Value: []byte(token), ID: tokenID(token)}
	if j.Template.Exchange.ExpiresAt != "" {
		if s, ok := jsonAt(resp, j.Template.Exchange.ExpiresAt).(string); ok {
			issued.ExpiresAt, _ = time.Parse(time.RFC3339, s)
		}
	}
	if issued.ExpiresAt.IsZero() {
		issued.ExpiresAt = j.now().Add(time.Hour)
	}
	j.mu.Lock()
	if j.tokens == nil {
		j.tokens, j.scopes = map[string]Issued{}, map[string]string{}
	}
	j.tokens[scopeKey] = issued
	j.scopes[issued.ID] = scopeKey
	j.mu.Unlock()
	return issued, nil
}

// Revoke ends an issued token, trying once more when the first try fails.
func (j *JWTExchange) Revoke(ctx context.Context, issued Issued) error {
	j.mu.Lock()
	if scopeKey, ok := j.scopes[issued.ID]; ok {
		delete(j.tokens, scopeKey)
		delete(j.scopes, issued.ID)
	}
	j.mu.Unlock()
	if j.Template.Revoke == nil {
		return nil
	}
	var err error
	for try := 0; try < 2; try++ {
		if _, err = j.do(ctx, *j.Template.Revoke, map[string]any{}, "", string(issued.Value)); err == nil {
			return nil
		}
	}
	return fmt.Errorf("revoking a token: %w", err)
}

func (j *JWTExchange) lookup(ctx context.Context, key *rsa.PrivateKey, values map[string]any) (map[string]any, error) {
	cacheKey := fmt.Sprint(values["owner"], "/", values["repo"])
	j.mu.Lock()
	if found, ok := j.lookups[cacheKey]; ok && j.now().Sub(found.at) < lookupLife {
		j.mu.Unlock()
		return found.values, nil
	}
	j.mu.Unlock()
	jwt, err := j.signJWT(key, values)
	if err != nil {
		return nil, err
	}
	resp, err := j.do(ctx, *j.Template.Lookup, values, jwt, "")
	if err != nil {
		return nil, fmt.Errorf("looking up %s: %w", cacheKey, err)
	}
	found := map[string]any{}
	for name, path := range j.Template.Lookup.Set {
		v := jsonAt(resp, path)
		if v == nil {
			return nil, fmt.Errorf("looking up %s: the response has no %s", cacheKey, path)
		}
		if f, ok := v.(float64); ok && f == float64(int64(f)) {
			v = int64(f)
		}
		found[name] = v
	}
	j.mu.Lock()
	if j.lookups == nil {
		j.lookups = map[string]lookedUp{}
	}
	j.lookups[cacheKey] = lookedUp{values: found, at: j.now()}
	j.mu.Unlock()
	return found, nil
}

func (j *JWTExchange) value(values map[string]any) func(string) (any, bool) {
	return func(name string) (any, bool) {
		if strings.HasPrefix(name, "settings.") {
			v, ok := j.Settings[strings.TrimPrefix(name, "settings.")]
			return v, ok && v != ""
		}
		if m := nowPlaceholder.FindStringSubmatch(name); m != nil {
			offset, _ := strconv.Atoi(m[1])
			return j.now().Unix() + int64(offset), true
		}
		v, ok := values[name]
		return v, ok
	}
}

func (j *JWTExchange) signJWT(key *rsa.PrivateKey, values map[string]any) (string, error) {
	claims, err := fill(j.Template.JWT.Claims, j.value(values))
	if err != nil {
		return "", fmt.Errorf("jwt.claims: %w", err)
	}
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT"})
	body, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signing := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(body)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// do sends one templated request and decodes its JSON answer. Errors name
// the request's path and status, never a token.
func (j *JWTExchange) do(ctx context.Context, r TemplateRequest, values map[string]any, jwt, issued string) (any, error) {
	url, err := fill(r.URL, j.value(values))
	if err != nil {
		return nil, err
	}
	var body io.Reader
	if r.Body != nil {
		filled, err := fill(r.Body, j.value(values))
		if err != nil {
			return nil, err
		}
		data, err := json.Marshal(filled)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(data)
	}
	method := r.Method
	if method == "" {
		method = http.MethodGet
	}
	req, err := http.NewRequestWithContext(ctx, method, url.(string), body)
	if err != nil {
		return nil, err
	}
	for k, v := range j.Template.Headers {
		req.Header.Set(k, v)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	switch r.Auth {
	case "", "jwt":
		req.Header.Set("Authorization", "Bearer "+jwt)
	case "issued":
		req.Header.Set("Authorization", "Bearer "+issued)
	}
	resp, err := j.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, req.URL.Path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("%s %s: %s", method, req.URL.Path, resp.Status)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("%s %s: the answer is not JSON", method, req.URL.Path)
	}
	return out, nil
}

// rsaKey reads an RSA private key in PKCS #1 or PKCS #8 PEM.
func rsaKey(data []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("the credential's secret is not a PEM private key")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("the credential's secret is not an RSA private key")
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("the credential's secret is not an RSA private key")
	}
	return key, nil
}

func scopeCacheKey(values map[string]any) string {
	data, _ := json.Marshal(values) // map keys are sorted
	return string(data)
}

// tokenID names a token in caches and the audit log without its value.
func tokenID(token string) string {
	sum := sha256.Sum256([]byte(token))
	return "tok-" + hex.EncodeToString(sum[:6])
}
