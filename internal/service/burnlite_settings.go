package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

var (
	// ErrBurnliteInput: an address or a project key the person gave is not one.
	ErrBurnliteInput = errors.New("invalid input")
	// ErrProjectsFromConfig: burnlite.projects is set, so the web UI cannot
	// change the projects.
	ErrProjectsFromConfig = errors.New("Burnlite's projects are set in config.yaml (burnlite.projects): change them there")
	// ErrSecretsNeedPassphrase: the secret store needs a person to unlock it,
	// but Burnlite's refresh token is rewritten while nobody watches.
	ErrSecretsNeedPassphrase = errors.New("Legato's secret store is unlocked by a passphrase, but Burnlite's refresh token is rewritten every hour as Legato syncs. Set secrets.master_key to a source Legato can read alone (auto, systemd-creds, secret-service, keychain or file)")
	// ErrUnknownConnection: the browser came back for a connection attempt
	// Legato does not have, or no longer has.
	ErrUnknownConnection = errors.New("this connection attempt is unknown or too old: connect again")
)

// pendingTTL is how long a connection attempt waits for the browser.
const pendingTTL = 10 * time.Minute

// BurnliteSettingsOptions are what the configuration says of Burnlite.
type BurnliteSettingsOptions struct {
	// ConfiguredURL is burnlite.url, the address offered for connecting.
	ConfiguredURL string
	// ConfiguredProjects is burnlite.projects; when set, the web UI cannot
	// change them.
	ConfiguredProjects []string
	// SecretSource says what unlocks Legato's secret store ("passphrase",
	// "auto", ...).
	SecretSource func(ctx context.Context) (string, error)
	// Connections is the connection the board shares with agents' MCP
	// connection to Burnlite (LEG-544).
	Connections *BurnliteConnections
}

// BurnliteOverview is what the web UI's settings show of Burnlite.
type BurnliteOverview struct {
	ConfiguredURL string `json:"configured_url,omitempty"`
	// Projects are searched for import; none is every project.
	Projects           []string     `json:"projects"`
	ProjectsFromConfig bool         `json:"projects_from_config"`
	Connection         BurnliteLink `json:"connection"`
}

// BurnliteSettings connects Legato to Burnlite from the web UI, where the
// browser comes back to Legato's own web server, and keeps the projects
// the web UI is given.
type BurnliteSettings struct {
	db        *store.Store
	connector *IntegrationConnector
	opts      BurnliteSettingsOptions
	now       func() time.Time

	mu      sync.Mutex
	pending map[string]pendingBurnlite
}

type pendingBurnlite struct {
	conn    *PendingConnection
	expires time.Time
}

// NewBurnliteSettings is Burnlite's settings over the database and the connector.
func NewBurnliteSettings(db *store.Store, connector *IntegrationConnector, opts BurnliteSettingsOptions) *BurnliteSettings {
	return &BurnliteSettings{db: db, connector: connector, opts: opts, now: time.Now, pending: map[string]pendingBurnlite{}}
}

// Overview is Burnlite's configuration, projects and connection.
func (b *BurnliteSettings) Overview(ctx context.Context) (BurnliteOverview, error) {
	o := BurnliteOverview{
		ConfiguredURL:      b.opts.ConfiguredURL,
		Projects:           BurnliteProjects(ctx, b.db, b.opts.ConfiguredProjects),
		ProjectsFromConfig: len(b.opts.ConfiguredProjects) > 0,
	}
	if o.Projects == nil {
		o.Projects = []string{}
	}
	link, err := b.opts.Connections.Current(ctx)
	if err != nil {
		return BurnliteOverview{}, err
	}
	o.Connection = link
	return o, nil
}

// Begin starts connecting to the Burnlite at address, the browser to come
// back to redirectURI, and gives the address the person opens to consent.
func (b *BurnliteSettings) Begin(ctx context.Context, address, redirectURI string) (string, error) {
	u, err := url.Parse(address)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", fmt.Errorf("%w: %q is not a Burnlite address, like https://burnlite.example.com", ErrBurnliteInput, address)
	}
	if b.opts.SecretSource != nil {
		source, err := b.opts.SecretSource(ctx)
		if err != nil {
			return "", err
		}
		if source == "passphrase" {
			return "", ErrSecretsNeedPassphrase
		}
	}
	client, err := b.opts.Connections.ConnectClient(ctx, address)
	if err != nil {
		return "", err
	}
	pending, err := b.connector.BeginWith(ctx, ConnectRequest{Name: BurnliteIntegration, URL: address, ClientName: "Legato", Scope: BurnliteScope, RedirectURI: redirectURI, HTTP: client})
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrBurnliteInput, err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	for state, p := range b.pending {
		if now.After(p.expires) {
			delete(b.pending, state)
		}
	}
	b.pending[pending.State()] = pendingBurnlite{conn: pending, expires: now.Add(pendingTTL)}
	return pending.URL(), nil
}

// Callback finishes the connection attempt that the address the browser
// came back to names, by its state. Its tokens become the connection the
// board shares with agents' MCP connection to Burnlite.
func (b *BurnliteSettings) Callback(ctx context.Context, redirected string) (BurnliteLink, error) {
	u, err := url.Parse(redirected)
	if err != nil {
		return BurnliteLink{}, ErrUnknownConnection
	}
	state := u.Query().Get("state")
	b.mu.Lock()
	p, ok := b.pending[state]
	delete(b.pending, state)
	b.mu.Unlock()
	if !ok || b.now().After(p.expires) {
		return BurnliteLink{}, ErrUnknownConnection
	}
	defer p.conn.Close()
	if err := p.conn.Paste(redirected); err != nil {
		return BurnliteLink{}, fmt.Errorf("Burnlite did not let Legato in: %w", err)
	}
	auth, err := p.conn.FinishTokens(ctx)
	if err != nil {
		return BurnliteLink{}, err
	}
	return b.opts.Connections.Adopt(ctx, p.conn.Address(), auth)
}

// SaveProjects keeps the projects searched for import.
func (b *BurnliteSettings) SaveProjects(ctx context.Context, projects []string) (BurnliteOverview, error) {
	if len(b.opts.ConfiguredProjects) > 0 {
		return BurnliteOverview{}, ErrProjectsFromConfig
	}
	if err := SaveBurnliteProjects(ctx, b.db, projects); err != nil {
		return BurnliteOverview{}, fmt.Errorf("%w: %v", ErrBurnliteInput, err)
	}
	return b.Overview(ctx)
}

// Disconnect disconnects the board and agents' MCP connection to Burnlite,
// revoking the shared tokens at Burnlite where it can.
func (b *BurnliteSettings) Disconnect(ctx context.Context) (OAuthDisconnect, error) {
	return b.opts.Connections.Disconnect(ctx)
}
