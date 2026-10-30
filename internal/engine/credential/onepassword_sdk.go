package credential

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// OnePasswordAPI is what Legato asks of 1Password through the SDK.
type OnePasswordAPI interface {
	Resolve(ctx context.Context, ref string) (string, error)
	Vaults(ctx context.Context) ([]OnePasswordVault, error)
	Items(ctx context.Context, vaultID string) ([]OnePasswordItem, error)
	Fields(ctx context.Context, vaultID, itemID string) ([]OnePasswordField, error)
}

// OnePasswordVault is a vault the service account can see.
type OnePasswordVault struct {
	ID    string
	Title string
}

// OnePasswordField is a field of an item. PublicKey is set on an SSH key's
// private key field; Value is left empty for fields that are secret.
type OnePasswordField struct {
	Title     string
	Value     string
	PublicKey string
}

// OnePasswordSDKStore reads op://vault/item/field with 1Password's Go SDK,
// as a service account, so no op command is needed. The token is Token, or
// the content of TokenFile, read for each read.
type OnePasswordSDKStore struct {
	Token     string
	TokenFile string
	// Connect makes a client for a token; nil uses the SDK.
	Connect func(ctx context.Context, token string) (OnePasswordAPI, error)

	mu sync.Mutex
	// api is the client for token. Making one takes about a second, so it
	// is kept until the token changes.
	api   OnePasswordAPI
	token string
}

func (s *OnePasswordSDKStore) client(ctx context.Context) (OnePasswordAPI, error) {
	token, err := serviceAccountToken(s.Token, s.TokenFile)
	if err != nil {
		return nil, err
	}
	if token == "" {
		return nil, fmt.Errorf("1Password: no service account token; set secrets.onepassword.token_file (or token)")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.api != nil && s.token == token {
		return s.api, nil
	}
	connect := s.Connect
	if connect == nil {
		connect = ConnectOnePassword
	}
	api, err := connect(ctx, token)
	if err != nil {
		return nil, fmt.Errorf("1Password: could not sign in with the service account of secrets.onepassword: %s", withoutToken(err, token))
	}
	s.api, s.token = api, token
	return api, nil
}

// withoutToken is err's message with the token taken out. 1Password's
// messages say what went wrong and name no secret, but a token is never
// put in an error.
func withoutToken(err error, token string) string {
	msg := err.Error()
	if token != "" {
		msg = strings.ReplaceAll(msg, token, "…")
	}
	return msg
}

func (s *OnePasswordSDKStore) currentToken() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.token
}

func (s *OnePasswordSDKStore) Get(ctx context.Context, ref Ref) (Secret, error) {
	api, err := s.client(ctx)
	if err != nil {
		return Secret{}, err
	}
	value, err := api.Resolve(ctx, ref.String())
	if err != nil {
		return Secret{}, fmt.Errorf("1Password could not read %s: %s", ref, withoutToken(err, s.currentToken()))
	}
	return Secret{Type: TypeValue, Value: trimNewline([]byte(value))}, nil
}

// ListItems lists a vault's items of one category, named as op names it
// ("SSH Key"). The vault is a name or an identifier.
func (s *OnePasswordSDKStore) ListItems(ctx context.Context, vault, category string) ([]OnePasswordItem, error) {
	api, err := s.client(ctx)
	if err != nil {
		return nil, err
	}
	vaults, err := api.Vaults(ctx)
	if err != nil {
		return nil, fmt.Errorf("1Password could not list its vaults: %s", withoutToken(err, s.currentToken()))
	}
	vaultID := ""
	for _, v := range vaults {
		if v.ID == vault || v.Title == vault {
			vaultID = v.ID
			break
		}
	}
	if vaultID == "" {
		return nil, fmt.Errorf("1Password has no vault %s that the service account can read", vault)
	}
	all, err := api.Items(ctx, vaultID)
	if err != nil {
		return nil, fmt.Errorf("1Password could not list vault %s: %s", vault, withoutToken(err, s.currentToken()))
	}
	want := categoryKey(category)
	var items []OnePasswordItem
	for _, item := range all {
		if categoryKey(item.Category) == want {
			items = append(items, item)
		}
	}
	return items, nil
}

// categoryKey lets op's category names ("SSH Key") match the SDK's ("SshKey").
func categoryKey(name string) string {
	return strings.ToLower(strings.ReplaceAll(name, " ", ""))
}

// Field reads one field of an item that is not secret: "public key" is an
// SSH key's public key. Secrets are read with Get.
func (s *OnePasswordSDKStore) Field(ctx context.Context, vault, item, label string) (string, error) {
	api, err := s.client(ctx)
	if err != nil {
		return "", err
	}
	fields, err := api.Fields(ctx, vault, item)
	if err != nil {
		return "", fmt.Errorf("1Password could not read the %s of item %s: %s", label, item, withoutToken(err, s.currentToken()))
	}
	for _, f := range fields {
		if strings.EqualFold(f.Title, label) && f.Value != "" {
			return f.Value, nil
		}
	}
	if strings.EqualFold(label, "public key") {
		for _, f := range fields {
			if f.PublicKey != "" {
				return strings.TrimSpace(f.PublicKey), nil
			}
		}
	}
	return "", fmt.Errorf("1Password item %s has no %s", item, label)
}
