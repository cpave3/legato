package credential

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// githubTemplate is the exchange the github profile ships, with the API
// address left for the test to fill in.
const githubTemplate = `
headers:
  Accept: application/vnd.github+json
  X-GitHub-Api-Version: "2022-11-28"
jwt:
  alg: RS256
  claims: { iss: "{settings.client_id}", iat: "{now-60}", exp: "{now+540}" }
lookup:
  url: "API/repos/{owner}/{repo}/installation"
  auth: jwt
  set: { installation: "$.id" }
exchange:
  method: POST
  url: "API/app/installations/{installation}/access_tokens"
  auth: jwt
  body: { repositories: ["{repo}"], permissions: "{permissions}" }
  token: "$.token"
  expires_at: "$.expires_at"
revoke:
  method: DELETE
  url: "API/installation/token"
  auth: issued
`

func appKey(t *testing.T) (*rsa.PrivateKey, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return key, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
}

// fakeGitHub answers the App endpoints and records what it was asked.
type fakeGitHub struct {
	mu       sync.Mutex
	key      *rsa.PublicKey
	lookups  int
	issued   []map[string]any
	revoked  []string
	failNext bool
	next     int
}

func (g *fakeGitHub) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()
		auth := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		switch {
		case r.Method == "GET" && r.URL.Path == "/repos/cpave3/legato/installation":
			g.checkJWT(t, auth)
			g.lookups++
			_, _ = io.WriteString(w, `{"id": 4242}`)
		case r.Method == "POST" && r.URL.Path == "/app/installations/4242/access_tokens":
			g.checkJWT(t, auth)
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			g.issued = append(g.issued, body)
			g.next++
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "ghs_token" + string(rune('0'+g.next)), "expires_at": "2026-10-29T13:00:00Z"})
		case r.Method == "DELETE" && r.URL.Path == "/installation/token":
			if g.failNext {
				g.failNext = false
				w.WriteHeader(502)
				return
			}
			g.revoked = append(g.revoked, auth)
			w.WriteHeader(204)
		default:
			w.WriteHeader(404)
		}
	})
}

func (g *fakeGitHub) checkJWT(t *testing.T, token string) {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Errorf("not a JWT: %q", token)
		return
	}
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(g.key, crypto.SHA256, sum[:], sig); err != nil {
		t.Errorf("the JWT's signature does not verify: %v", err)
	}
	var header, claims map[string]any
	h, _ := base64.RawURLEncoding.DecodeString(parts[0])
	c, _ := base64.RawURLEncoding.DecodeString(parts[1])
	_ = json.Unmarshal(h, &header)
	_ = json.Unmarshal(c, &claims)
	if header["alg"] != "RS256" || claims["iss"] != "Iv1.client" {
		t.Errorf("header %v claims %v", header, claims)
	}
	if iat, exp := claims["iat"].(float64), claims["exp"].(float64); exp-iat != 600 {
		t.Errorf("iat %v exp %v: want a JWT of 10 minutes, back-dated a minute", iat, exp)
	}
}

func newExchange(t *testing.T) (*JWTExchange, *fakeGitHub, Secret, *time.Time) {
	t.Helper()
	key, keyPEM := appKey(t)
	gh := &fakeGitHub{key: &key.PublicKey}
	srv := httptest.NewTLSServer(gh.handler(t))
	t.Cleanup(srv.Close)
	var tmpl ExchangeTemplate
	if err := yaml.Unmarshal([]byte(strings.ReplaceAll(githubTemplate, "API", srv.URL)), &tmpl); err != nil {
		t.Fatal(err)
	}
	if err := tmpl.Validate(); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	ex := &JWTExchange{Template: tmpl, Settings: map[string]string{"client_id": "Iv1.client"}, Client: srv.Client(), Now: func() time.Time { return now }}
	return ex, gh, Secret{Type: TypeKeyPair, Value: keyPEM}, &now
}

func repoScope(op string, permissions map[string]string) Scope {
	return Scope{Operations: []string{op}, Resources: []string{"github:cpave3/legato"}, Values: map[string]any{"owner": "cpave3", "repo": "legato", "permissions": permissions}}
}

func TestJWTExchangeMakesATokenForOneRepository(t *testing.T) {
	ex, gh, secret, _ := newExchange(t)
	issued, err := ex.Issue(context.Background(), secret, repoScope("git.read", map[string]string{"contents": "read", "metadata": "read"}))
	if err != nil {
		t.Fatal(err)
	}
	if string(issued.Value) != "ghs_token1" || !issued.ExpiresAt.Equal(time.Date(2026, 10, 29, 13, 0, 0, 0, time.UTC)) {
		t.Fatalf("issued = %q %v", issued.Value, issued.ExpiresAt)
	}
	body := gh.issued[0]
	repos, _ := body["repositories"].([]any)
	perms, _ := body["permissions"].(map[string]any)
	if len(repos) != 1 || repos[0] != "legato" || perms["contents"] != "read" || len(perms) != 2 {
		t.Fatalf("exchange body = %v", body)
	}
}

func TestATokenIsReusedWhileItLastsAndItsScopeMatches(t *testing.T) {
	ex, gh, secret, now := newExchange(t)
	read := map[string]string{"contents": "read", "metadata": "read"}
	ctx := context.Background()
	first, _ := ex.Issue(ctx, secret, repoScope("git.read", read))
	second, _ := ex.Issue(ctx, secret, repoScope("api.read", read))
	if string(second.Value) != string(first.Value) || len(gh.issued) != 1 {
		t.Fatalf("a matching token was not reused: %q %q, %d issued", first.Value, second.Value, len(gh.issued))
	}
	if gh.lookups != 1 {
		t.Fatalf("lookups = %d", gh.lookups)
	}
	write, _ := ex.Issue(ctx, secret, repoScope("git.push", map[string]string{"contents": "write"}))
	if string(write.Value) == string(first.Value) {
		t.Fatal("a write was given the read token")
	}
	*now = now.Add(56 * time.Minute) // 4 minutes left: too little
	third, _ := ex.Issue(ctx, secret, repoScope("git.read", read))
	if string(third.Value) == string(first.Value) {
		t.Fatal("a token with 4 minutes left was reused")
	}
	if gh.lookups != 1 {
		t.Fatalf("the installation was looked up again within the hour: %d", gh.lookups)
	}
}

func TestAnIssuedTokenIsRevokedWithItself(t *testing.T) {
	ex, gh, secret, _ := newExchange(t)
	ctx := context.Background()
	issued, _ := ex.Issue(ctx, secret, repoScope("git.push", map[string]string{"contents": "write"}))
	gh.failNext = true // the first try fails; it is tried once more
	if err := ex.Revoke(ctx, issued); err != nil {
		t.Fatal(err)
	}
	if len(gh.revoked) != 1 || gh.revoked[0] != string(issued.Value) {
		t.Fatalf("revoked = %v", gh.revoked)
	}
	// A revoked token is not handed out again.
	again, _ := ex.Issue(ctx, secret, repoScope("git.push", map[string]string{"contents": "write"}))
	if string(again.Value) == string(issued.Value) {
		t.Fatal("a revoked token was reused")
	}
}

func TestATemplateCannotChooseItsHost(t *testing.T) {
	for name, body := range map[string]string{
		"placeholder in the host": "exchange: { url: 'https://{settings.host}/x', token: '$.t' }",
		"plain http":              "exchange: { url: 'http://api.example.com/x', token: '$.t' }",
		"unknown placeholder":     "exchange: { url: 'https://api.example.com/{nope}', token: '$.t' }",
		"no token path":           "exchange: { url: 'https://api.example.com/x' }",
		"bad auth":                "exchange: { url: 'https://api.example.com/x', token: '$.t', auth: cookie }",
	} {
		var tmpl ExchangeTemplate
		if err := yaml.Unmarshal([]byte(body), &tmpl); err != nil {
			t.Fatal(err)
		}
		if err := tmpl.Validate(); err == nil {
			t.Errorf("%s: valid", name)
		}
	}
}
