package credential

import (
	"context"
	"regexp"
	"strings"
	"time"

	onepassword "github.com/1password/onepassword-sdk-go"
)

// OnePasswordIntegrationVersion is the version Legato gives 1Password when
// it signs in, shown in the service account's activity. The binary sets it.
var OnePasswordIntegrationVersion = "dev"

// ConnectOnePassword signs in to 1Password with a service account token,
// through the SDK. Signing in starts the SDK's core, which takes about a
// second.
func ConnectOnePassword(ctx context.Context, token string) (OnePasswordAPI, error) {
	client, err := onepassword.NewClient(ctx,
		onepassword.WithServiceAccountToken(token),
		onepassword.WithIntegrationInfo("Legato", onePasswordIntegrationVersion(OnePasswordIntegrationVersion)))
	if err != nil {
		return nil, err
	}
	return sdkOnePassword{client: client}, nil
}

// pseudoVersion is a Go pseudo-version: the base version, a timestamp and
// a 12-character commit hash.
var pseudoVersion = regexp.MustCompile(`^(v\d+\.\d+\.\d+)-(?:[0-9A-Za-z.-]*\.)?\d{14}-([0-9a-f]{12})$`)

// onePasswordIntegrationVersion shortens a Go build's version to one that
// 1Password accepts: at most 20 characters, without '+'. Build metadata
// (+dirty, +incompatible) is dropped and a pseudo-version keeps its base
// version and a short commit hash.
func onePasswordIntegrationVersion(v string) string {
	v, _, _ = strings.Cut(v, "+")
	if m := pseudoVersion.FindStringSubmatch(v); m != nil {
		v = m[1] + "-" + m[2][:7]
	}
	v = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' {
			return r
		}
		return -1
	}, v)
	if len(v) > 20 {
		v = v[:20]
	}
	if v == "" {
		return "dev"
	}
	return v
}

// sdkOnePassword is OnePasswordAPI over the SDK's client.
type sdkOnePassword struct{ client *onepassword.Client }

func (s sdkOnePassword) Resolve(ctx context.Context, ref string) (string, error) {
	return s.client.Secrets().Resolve(ctx, ref)
}

func (s sdkOnePassword) Vaults(ctx context.Context) ([]OnePasswordVault, error) {
	list, err := s.client.Vaults().List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]OnePasswordVault, 0, len(list))
	for _, v := range list {
		out = append(out, OnePasswordVault{ID: v.ID, Title: v.Title})
	}
	return out, nil
}

func (s sdkOnePassword) Items(ctx context.Context, vaultID string) ([]OnePasswordItem, error) {
	list, err := s.client.Items().List(ctx, vaultID)
	if err != nil {
		return nil, err
	}
	out := make([]OnePasswordItem, 0, len(list))
	for _, item := range list {
		out = append(out, OnePasswordItem{
			ID: item.ID, Title: item.Title, VaultID: item.VaultID, Category: string(item.Category),
			UpdatedAt: item.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}
	return out, nil
}

// publicFieldTypes are the field types whose values are not secret.
var publicFieldTypes = map[onepassword.ItemFieldType]bool{
	onepassword.ItemFieldTypeText: true, onepassword.ItemFieldTypeURL: true,
	onepassword.ItemFieldTypeEmail: true, onepassword.ItemFieldTypePhone: true,
}

// Fields gives an item's fields without the values of secret ones, and an
// SSH key's public key.
func (s sdkOnePassword) Fields(ctx context.Context, vaultID, itemID string) ([]OnePasswordField, error) {
	item, err := s.client.Items().Get(ctx, vaultID, itemID)
	if err != nil {
		return nil, err
	}
	out := make([]OnePasswordField, 0, len(item.Fields))
	for _, f := range item.Fields {
		field := OnePasswordField{Title: f.Title}
		if publicFieldTypes[f.FieldType] {
			field.Value = f.Value
		}
		if f.Details != nil {
			if key := f.Details.SSHKey(); key != nil {
				field.PublicKey = key.PublicKey
			}
		}
		out = append(out, field)
	}
	return out, nil
}
