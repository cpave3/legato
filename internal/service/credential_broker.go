package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/credential"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

// ApprovalSourceCredential is the source of approvals for credential access.
const ApprovalSourceCredential = "credential"

// What a request for access came to.
const (
	AccessGranted = "granted"
	AccessPending = "pending"
	AccessDenied  = "denied"
)

// The grants an approval can leave, from the loosest.
const (
	grantSession = "session"
	grantWindow  = "window"
	grantOnce    = "once"
	// grantAuto records what an auto rule allowed. It answers a request but
	// never allows a use: the rule decides each use, within its limits.
	grantAuto = "auto"
)

// defaultWindow is the window a session rule offers as a stricter choice.
const defaultWindow = 30 * time.Minute

// AccessRequest is an agent asking for a scope of a credential.
type AccessRequest struct {
	TaskID      string
	ExecutionID string
	Credential  string
	Operations  []string
	Resources   []string
	Reason      string
	Blocking    bool
	// amounts are those of the use a request is made for, so a rule whose
	// limit the use would pass gives way as it does for the use.
	amounts map[string]int64
	// noWait asks the person and returns pending at once, for a channel
	// that cannot hold its caller (a git push).
	noWait bool
}

// AccessResult is what a request came to.
type AccessResult struct {
	Status     string `json:"status"`
	ApprovalID string `json:"approval_id,omitempty"`
	Reason     string `json:"reason,omitempty"`
	// Grant says for how long, when granted: "this session", "30m", "once".
	Grant string `json:"grant,omitempty"`
}

// CredentialUse is one use of a credential.
type CredentialUse struct {
	TaskID      string
	ExecutionID string
	Credential  string
	Operation   string
	Resource    string
	Amounts     map[string]int64
	// Call is what the channel saw, for the audit log: "POST /v1/voices".
	Call string
}

// UseDecision says whether a use may go ahead.
type UseDecision struct {
	Allowed    bool   `json:"allowed"`
	ApprovalID string `json:"approval_id,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// AgentMessenger tells an agent something, queued if it is at work.
type AgentMessenger interface {
	QueueMessage(ctx context.Context, taskID, message string) error
}

// CredentialBrokerOptions configures a CredentialBroker.
type CredentialBrokerOptions struct {
	Credentials []config.CredentialConfig
	// Sources are vaults whose SSH Key items are credentials too.
	Sources []config.CredentialSourceConfig
	// ProfileDirs hold profiles that add to or replace the shipped ones.
	ProfileDirs []string
	// Stores read secrets by scheme. Missing schemes get the built-in stores.
	Stores map[string]credential.Store
	// OnePasswordToken is the service account token for op:// references,
	// or OnePasswordTokenFile the file that holds it.
	OnePasswordToken     string
	OnePasswordTokenFile string
	// Messenger tells an agent of a decision it no longer waits for. Only a
	// host process (TUI, legato serve) sets it.
	Messenger AgentMessenger
	// WaitTimeout is how long a request waits for the person. Default 60s.
	WaitTimeout time.Duration
	// HTTPClient reaches token endpoints and APIs (nil: a default one).
	HTTPClient *http.Client
	Now        func() time.Time
}

// CredentialBroker decides what an agent may do with a credential, keeps the
// grants it holds, and records every request and use. Every Legato process
// that acts for an agent builds one over the same database.
type CredentialBroker struct {
	db          *store.Store
	approvals   *ApprovalService
	credentials map[string]brokerCredential // configured; fixed after start
	names       []string
	stores      map[string]credential.Store

	// Credentials from vaults change while Legato runs.
	sources     []brokerSource
	mu          sync.RWMutex
	fromSources map[string]brokerCredential
	refreshMu   sync.Mutex
	refreshedAt time.Time
	messenger   AgentMessenger
	wait        time.Duration
	now         func() time.Time
	httpClient  *http.Client
}

type brokerCredential struct {
	config  config.CredentialConfig
	ref     credential.Ref
	profile CredentialProfile
	rules   []config.CredentialRule
	// upstream and key, for a credential the http-proxy channel serves.
	upstream *url.URL
	key      credential.KeyPlace
	// source is the vault a credential came from; item is its item there.
	source string
	item   *credential.OnePasswordItem
	// issuer makes what the agent's tools use from the secret; jwt is it
	// when it makes tokens (jwt-exchange), with their cache.
	issuer credential.Issuer
	jwt    *credential.JWTExchange
}

// brokerSource is a vault whose items are credentials.
type brokerSource struct {
	config  config.CredentialSourceConfig
	vault   string
	profile CredentialProfile
	rules   []config.CredentialRule
}

// NewCredentialBroker checks the configured credentials against the
// profiles and builds the broker. A credential with an unknown profile, or a
// secret type its profile does not take, is an error.
func NewCredentialBroker(db *store.Store, approvals *ApprovalService, opts CredentialBrokerOptions) (*CredentialBroker, error) {
	profiles, err := LoadCredentialProfiles(opts.ProfileDirs...)
	if err != nil {
		return nil, err
	}
	b := &CredentialBroker{db: db, approvals: approvals, credentials: map[string]brokerCredential{}, messenger: opts.Messenger, wait: opts.WaitTimeout, now: opts.Now, httpClient: opts.HTTPClient}
	if b.wait <= 0 {
		b.wait = 60 * time.Second
	}
	if b.now == nil {
		b.now = time.Now
	}
	b.stores = map[string]credential.Store{
		"env": credential.EnvStore{}, "file": credential.FileStore{}, "systemd-creds": credential.SystemdCredsStore{},
		"op": credential.OnePasswordStore{Token: opts.OnePasswordToken, TokenFile: opts.OnePasswordTokenFile}, "keychain": credential.KeychainStore{},
	}
	for scheme, s := range opts.Stores {
		b.stores[scheme] = s
	}
	for _, c := range opts.Credentials {
		profile, ok := profiles[c.Profile]
		if !ok {
			return nil, fmt.Errorf("credentials %q: no profile %q", c.Name, c.Profile)
		}
		ref, err := credential.ParseRef(c.Secret)
		if err != nil {
			return nil, fmt.Errorf("credentials %q: %w", c.Name, err)
		}
		if c.Type != "" && !listHas(profile.Accepts, c.Type) {
			return nil, fmt.Errorf("credentials %q: profile %s does not take secrets of type %s", c.Name, profile.Name, c.Type)
		}
		rules := c.Policy
		if len(rules) == 0 {
			rules = profile.Policy
		}
		bc := brokerCredential{config: c, ref: ref, profile: profile, rules: expandRepositories(rules, c.Settings.Repositories), issuer: credential.StaticIssuer{}}
		if profile.Issuer == "jwt-exchange" {
			bc.jwt = &credential.JWTExchange{Template: *profile.JWTExchange, Settings: map[string]string{"client_id": c.Settings.ClientID}, Client: opts.HTTPClient, Now: b.now}
			bc.issuer = bc.jwt
		}
		if listHas(profile.Channels, ChannelHTTPProxy) {
			if err := bc.proxySettings(); err != nil {
				return nil, fmt.Errorf("credentials %q: %w", c.Name, err)
			}
		}
		if listHas(profile.Channels, ChannelSSHAgent) && c.Type != "" && c.Type != credential.TypeKeyPair {
			return nil, fmt.Errorf("credentials %q: profile ssh takes a keypair secret", c.Name)
		}
		b.credentials[c.Name] = bc
		b.names = append(b.names, c.Name)
	}
	sort.Strings(b.names)
	for _, src := range opts.Sources {
		profile, ok := profiles[src.Profile]
		if !ok {
			return nil, fmt.Errorf("credential_sources %s: no profile %q", src.Vault, src.Profile)
		}
		if !listHas(profile.Channels, ChannelSSHAgent) {
			return nil, fmt.Errorf("credential_sources %s: a vault gives SSH keys only for now (profile ssh)", src.Vault)
		}
		vault, err := credential.ParseVaultRef(src.Vault)
		if err != nil {
			return nil, fmt.Errorf("credential_sources: %w", err)
		}
		rules := src.Policy
		if len(rules) == 0 {
			rules = profile.Policy
		}
		b.sources = append(b.sources, brokerSource{config: src, vault: vault, profile: profile, rules: rules})
	}
	return b, nil
}

// expandRepositories turns a rule naming {repositories} into one rule for
// each repository a credential lists. With none listed the rule is dropped:
// nothing matches it, so nothing is allowed by it.
func expandRepositories(rules []config.CredentialRule, repositories []string) []config.CredentialRule {
	var out []config.CredentialRule
	for _, r := range rules {
		if !strings.Contains(r.Resource, "{repositories}") {
			out = append(out, r)
			continue
		}
		for _, repo := range repositories {
			each := r
			each.Resource = strings.ReplaceAll(r.Resource, "{repositories}", repo)
			out = append(out, each)
		}
	}
	return out
}

// sourceStaleAfter is how long a vault's list of keys is used before it is
// listed again, when a use needs it.
const sourceStaleAfter = 30 * time.Second

// sourceSSHCategory is the 1Password category of SSH keys.
const sourceSSHCategory = "SSH Key"

// vaultLister is a store that can list a vault's items (1Password).
type vaultLister interface {
	ListItems(ctx context.Context, vault, category string) ([]credential.OnePasswordItem, error)
	Field(ctx context.Context, vault, item, label string) (string, error)
}

// CredentialSourceSummary describes a vault credentials come from.
type CredentialSourceSummary struct {
	Vault    string   `json:"vault"`
	Profile  string   `json:"profile"`
	Channels []string `json:"channels"`
}

// Sources lists the vaults credentials come from.
func (b *CredentialBroker) Sources() []CredentialSourceSummary {
	out := make([]CredentialSourceSummary, 0, len(b.sources))
	for _, src := range b.sources {
		out = append(out, CredentialSourceSummary{Vault: src.config.Vault, Profile: src.profile.Name, Channels: src.profile.Channels})
	}
	return out
}

// hasSourceChannel reports whether a source gives credentials of a channel.
func (b *CredentialBroker) hasSourceChannel(channel string) bool {
	for _, src := range b.sources {
		if listHas(src.profile.Channels, channel) {
			return true
		}
	}
	return false
}

// RefreshSources lists every vault again. A vault that cannot be listed
// keeps the credentials it gave last.
func (b *CredentialBroker) RefreshSources(ctx context.Context) error {
	if len(b.sources) == 0 {
		return nil
	}
	b.refreshMu.Lock()
	defer b.refreshMu.Unlock()
	return b.refreshLocked(ctx)
}

// refreshIfStale lists the vaults again when the last list is older than
// sourceStaleAfter. Callers that race wait for one list.
func (b *CredentialBroker) refreshIfStale(ctx context.Context) {
	if len(b.sources) == 0 {
		return
	}
	b.refreshMu.Lock()
	defer b.refreshMu.Unlock()
	if !b.refreshedAt.IsZero() && b.now().Sub(b.refreshedAt) < sourceStaleAfter {
		return
	}
	if err := b.refreshLocked(ctx); err != nil {
		log.Printf("credentials: %v", err)
	}
}

func (b *CredentialBroker) refreshLocked(ctx context.Context) error {
	b.mu.RLock()
	previous := b.fromSources
	b.mu.RUnlock()
	next := map[string]brokerCredential{}
	var errs []string
	for _, src := range b.sources {
		lister, ok := b.stores["op"].(vaultLister)
		if !ok {
			errs = append(errs, "no 1Password store in this Legato")
			continue
		}
		items, err := lister.ListItems(ctx, src.vault, sourceSSHCategory)
		if err != nil {
			errs = append(errs, err.Error())
			for name, bc := range previous {
				if bc.source == src.config.Vault {
					next[name] = bc
				}
			}
			continue
		}
		for i := range items {
			item := items[i]
			name := itemCredentialName(item)
			if _, configured := b.credentials[name]; configured {
				continue
			}
			if _, taken := next[name]; taken {
				log.Printf("credentials: %s: two items are named %s; the first is used", src.config.Vault, name)
				continue
			}
			ref, err := credential.ParseRef("op://" + item.VaultID + "/" + item.ID + "/private key?ssh-format=openssh")
			if err != nil {
				continue
			}
			next[name] = brokerCredential{
				config: config.CredentialConfig{Name: name, Secret: ref.String(), Profile: src.profile.Name, Type: credential.TypeKeyPair, Policy: src.config.Policy},
				ref:    ref, profile: src.profile, rules: src.rules, source: src.config.Vault, item: &item,
			}
		}
	}
	b.mu.Lock()
	b.fromSources = next
	b.mu.Unlock()
	b.refreshedAt = b.now()
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

var credentialNameUnsafe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// itemCredentialName makes an item's title a credential name, or uses its
// identifier when nothing of the title is left.
func itemCredentialName(item credential.OnePasswordItem) string {
	name := strings.Trim(credentialNameUnsafe.ReplaceAllString(strings.TrimSpace(item.Title), "-"), "-._")
	if len(name) > 64 {
		name = name[:64]
	}
	if name == "" {
		return item.ID
	}
	return name
}

// lookup finds a credential by name: a configured one, else one from a
// vault, listing the vaults again once when it is not known.
func (b *CredentialBroker) lookup(ctx context.Context, name string) (brokerCredential, bool) {
	if c, ok := b.credentials[name]; ok {
		return c, true
	}
	b.mu.RLock()
	c, ok := b.fromSources[name]
	b.mu.RUnlock()
	if ok || len(b.sources) == 0 {
		return c, ok
	}
	b.refreshIfStale(ctx)
	b.mu.RLock()
	defer b.mu.RUnlock()
	c, ok = b.fromSources[name]
	return c, ok
}

// all lists every credential known now, by name.
func (b *CredentialBroker) all() []brokerCredential {
	out := make([]brokerCredential, 0, len(b.names))
	for _, name := range b.names {
		out = append(out, b.credentials[name])
	}
	b.mu.RLock()
	var extra []string
	for name := range b.fromSources {
		extra = append(extra, name)
	}
	sort.Strings(extra)
	for _, name := range extra {
		out = append(out, b.fromSources[name])
	}
	b.mu.RUnlock()
	sort.SliceStable(out, func(i, j int) bool { return out[i].config.Name < out[j].config.Name })
	return out
}

// proxySettings works out where the proxy sends a credential's requests and
// where the key goes: the credential's settings, else its profile's.
func (bc *brokerCredential) proxySettings() error {
	upstream := firstNonEmpty(bc.config.Settings.Upstream, bc.profile.Upstream)
	if upstream == "" {
		return fmt.Errorf("profile %s needs settings.upstream", bc.profile.Name)
	}
	u, err := url.Parse(upstream)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return fmt.Errorf("upstream %q: want an https address", upstream)
	}
	bc.upstream = u
	bc.key = bc.profile.Key
	if k := bc.config.Settings.Key; k != (config.CredentialKeyPlace{}) {
		bc.key = credential.KeyPlace{Header: k.Header, Format: k.Format, Query: k.Query}
	}
	if err := bc.key.Validate(); err != nil {
		return fmt.Errorf("profile %s: %w (settings.key)", bc.profile.Name, err)
	}
	return nil
}

// RegisterResolver makes this process apply decisions on credential
// approvals. Only a host process (TUI, legato serve) calls it.
func (b *CredentialBroker) RegisterResolver() {
	b.approvals.RegisterResolver(ApprovalSourceCredential, credentialResolver{b: b})
}

// StartEventLoop ends the grants of agents that end, until stop is called.
func (b *CredentialBroker) StartEventLoop(ctx context.Context, bus *events.Bus) (stop func()) {
	if bus == nil {
		return func() {}
	}
	ctx, cancel := context.WithCancel(ctx)
	died := bus.Subscribe(events.EventAgentDied)
	go func() {
		// A vault is listed at start and every two minutes, so the person
		// sees its keys; a use lists it again when the list is stale.
		var refresh <-chan time.Time
		if len(b.sources) > 0 {
			if err := b.RefreshSources(ctx); err != nil {
				log.Printf("credentials: %v", err)
			}
			tick := time.NewTicker(2 * time.Minute)
			defer tick.Stop()
			refresh = tick.C
		}
		for {
			select {
			case <-refresh:
				if err := b.RefreshSources(ctx); err != nil {
					log.Printf("credentials: %v", err)
				}
				continue
			case <-ctx.Done():
				return
			case _, ok := <-died:
				if !ok {
					return
				}
				b.endGrantsOfEndedAgents(ctx)
			}
		}
	}()
	return func() { cancel(); bus.Unsubscribe(died) }
}

func (b *CredentialBroker) endGrantsOfEndedAgents(ctx context.Context) {
	grants, err := b.db.ListLiveCredentialGrants(ctx, b.now())
	if err != nil {
		return
	}
	ended := map[string]bool{}
	for _, g := range grants {
		if ended[g.ExecutionID] {
			continue
		}
		if !b.executionRunning(ctx, g.ExecutionID, "") {
			ended[g.ExecutionID] = true
			_ = b.db.EndCredentialGrants(ctx, g.ExecutionID)
		}
	}
}

// CredentialSummary describes a configured credential, never its value.
type CredentialSummary struct {
	Name    string                  `json:"name"`
	Profile string                  `json:"profile"`
	Store   string                  `json:"store"`
	Secret  string                  `json:"secret"`
	Rules   []config.CredentialRule `json:"rules"`
	// Channels say how an agent gets it; Env names the variables a session
	// gets for it (a placeholder key and the proxy's address).
	Channels []string `json:"channels"`
	Env      []string `json:"env,omitempty"`
	// Source is the vault a credential came from, "" for a configured one.
	Source string `json:"source,omitempty"`
	// Git is the git host pushes of the credential go to (GitHub), and
	// Repositories the ones it may be used for.
	Git          string   `json:"git,omitempty"`
	Repositories []string `json:"repositories,omitempty"`
}

// Credentials lists the configured credentials.
func (b *CredentialBroker) Credentials() []CredentialSummary {
	all := b.all()
	out := make([]CredentialSummary, 0, len(all))
	for _, c := range all {
		out = append(out, CredentialSummary{Name: c.config.Name, Profile: c.profile.Name, Store: c.ref.Scheme, Secret: c.ref.String(), Rules: c.rules,
			Channels: c.profile.Channels, Env: c.envNames(), Source: c.source, Git: c.profile.Git, Repositories: c.config.Settings.Repositories})
	}
	return out
}

// DescribeCredentials summarizes configured credentials without a store,
// for text that tells agents about them.
func DescribeCredentials(credentials []config.CredentialConfig, sources []config.CredentialSourceConfig, profileDirs []string) ([]CredentialSummary, []CredentialSourceSummary, error) {
	b, err := NewCredentialBroker(nil, nil, CredentialBrokerOptions{Credentials: credentials, Sources: sources, ProfileDirs: profileDirs})
	if err != nil {
		return nil, nil, err
	}
	return b.Credentials(), b.Sources(), nil
}

// envNames are the variables a session gets for a proxied credential, with
// the key's variable first.
func (bc brokerCredential) envNames() []string {
	if !listHas(bc.profile.Channels, ChannelHTTPProxy) {
		return nil
	}
	var names []string
	for key, value := range bc.profile.Env {
		name := strings.ReplaceAll(key, "{NAME}", credentialEnvName(bc.config.Name))
		if strings.Contains(value, "{placeholder}") {
			names = append([]string{name}, names...)
		} else {
			names = append(names, name)
		}
	}
	if len(names) > 1 {
		sort.Strings(names[1:])
	}
	return names
}

// pair is one operation on one resource.
type pair struct{ operation, resource string }

func pairs(operations, resources []string) []pair {
	if len(resources) == 0 {
		resources = []string{""}
	}
	var out []pair
	for _, op := range operations {
		for _, r := range resources {
			out = append(out, pair{op, r})
		}
	}
	return out
}

// strictness orders modes from loosest to strictest.
var strictness = map[string]int{"auto": 0, "session": 1, "window": 2, "request": 3, "deny": 4}

// selectRule returns the most specific rule that matches a use, passing over
// rules whose limits are spent. nil when none matches.
func (b *CredentialBroker) selectRule(ctx context.Context, c brokerCredential, p pair, amounts map[string]int64) *config.CredentialRule {
	best, bestScore := -1, -1
	for i, r := range c.rules {
		if len(r.Operation) > 0 && !listHas(r.Operation, p.operation) {
			continue
		}
		if !credential.Covers(r.Resource, p.resource) {
			continue
		}
		if b.limitSpent(ctx, c.config.Name, r, amounts) {
			continue
		}
		score := credential.Specificity(r.Resource)
		if len(r.Operation) > 0 {
			score++
		}
		if score > bestScore {
			best, bestScore = i, score
		}
	}
	if best < 0 {
		return nil
	}
	return &c.rules[best]
}

// limitSpent reports whether a use of these amounts would take a rule's
// meters past a limit. A use counts one request whatever it says.
func (b *CredentialBroker) limitSpent(ctx context.Context, name string, r config.CredentialRule, amounts map[string]int64) bool {
	for _, limit := range r.Limits {
		total, err := b.db.CredentialMeterTotal(ctx, name, limit.Meter, b.now().Add(-limit.Period))
		if err != nil {
			continue
		}
		add := amounts[limit.Meter]
		if limit.Meter == "requests" {
			add = 1
		}
		if total+add > limit.Count || (add == 0 && total >= limit.Count) {
			return true
		}
	}
	return false
}

// meter records what an allowed use measured: one request and its amounts.
func (b *CredentialBroker) meter(ctx context.Context, name string, amounts map[string]int64) {
	if err := b.db.AddCredentialMeter(ctx, name, "requests", 1, b.now()); err != nil {
		log.Printf("credentials: meter: %v", err)
	}
	b.RecordMeasured(ctx, name, amounts)
}

// RecordMeasured adds what a channel measured after a use ended (response
// bytes) to the credential's totals. Requests are counted by the use.
func (b *CredentialBroker) RecordMeasured(ctx context.Context, name string, amounts map[string]int64) {
	for meter, amount := range amounts {
		if meter == "requests" || amount <= 0 {
			continue
		}
		if err := b.db.AddCredentialMeter(ctx, name, meter, amount, b.now()); err != nil {
			log.Printf("credentials: meter: %v", err)
		}
	}
}

// executionRunning reports whether an execution runs, for taskID when set.
func (b *CredentialBroker) executionRunning(ctx context.Context, executionID, taskID string) bool {
	if executionID == "" {
		return false
	}
	execution, err := b.db.GetExecution(ctx, executionID)
	if err != nil || execution.Status != store.ExecutionRunning {
		return false
	}
	return taskID == "" || taskID == execution.TaskID || taskID == execution.ExecutionTaskID || taskID == execution.SwarmSubtaskID
}

// Request asks for a scope. Rules that say auto grant at once and rules that
// say deny refuse; otherwise the person is asked, and Request waits up to the
// wait timeout for the decision before it returns pending.
func (b *CredentialBroker) Request(ctx context.Context, req AccessRequest) (AccessResult, error) {
	c, ok := b.lookup(ctx, req.Credential)
	if !ok {
		return AccessResult{}, fmt.Errorf("no credential named %q (legato credential list names them)", req.Credential)
	}
	if len(req.Operations) == 0 {
		return AccessResult{}, errors.New("a request names at least one operation")
	}
	b.audit(ctx, req.TaskID, req.ExecutionID, store.CredentialAuditEntry{Event: "request", Credential: req.Credential, Operation: strings.Join(req.Operations, ","), Resource: strings.Join(req.Resources, " "), Detail: clip(req.Reason, 500)})
	if !b.executionRunning(ctx, req.ExecutionID, req.TaskID) {
		return b.refuse(ctx, req, "the agent is not running"), nil
	}
	ps := pairs(req.Operations, req.Resources)
	var strictest *config.CredentialRule
	for _, p := range ps {
		r := b.selectRule(ctx, c, p, req.amounts)
		if r == nil {
			return b.refuse(ctx, req, fmt.Sprintf("no rule of %s allows %s on %s", req.Credential, p.operation, orAll(p.resource))), nil
		}
		if r.Approve == "deny" {
			return b.refuse(ctx, req, fmt.Sprintf("%s does not allow %s on %s", req.Credential, p.operation, orAll(p.resource))), nil
		}
		if strictest == nil || strictness[r.Approve] > strictness[strictest.Approve] {
			strictest = r
		}
	}
	if b.covered(ctx, req.ExecutionID, req.Credential, ps) {
		return AccessResult{Status: AccessGranted, Grant: "already granted"}, nil
	}
	if strictest.Approve == "auto" {
		if err := b.grant(ctx, req, grantAuto, 0, "", "rule"); err != nil {
			return AccessResult{}, err
		}
		return AccessResult{Status: AccessGranted, Grant: "this session"}, nil
	}
	approval, err := b.submit(ctx, req, c, *strictest)
	if err != nil {
		return AccessResult{}, err
	}
	b.audit(ctx, req.TaskID, req.ExecutionID, store.CredentialAuditEntry{Event: "pending", Credential: req.Credential, Operation: strings.Join(req.Operations, ","), Resource: strings.Join(req.Resources, " "), ApprovalID: approval.ID})
	if req.noWait {
		return AccessResult{Status: AccessPending, ApprovalID: approval.ID}, nil
	}
	decided, err := b.approvals.Wait(ctx, approval.ID, b.wait)
	if err != nil {
		return AccessResult{}, err
	}
	switch decided.Status {
	case store.ApprovalApproved:
		return AccessResult{Status: AccessGranted, ApprovalID: approval.ID, Grant: grantPhrase(decided)}, nil
	case store.ApprovalPending:
		return AccessResult{Status: AccessPending, ApprovalID: approval.ID}, nil
	default:
		note := ""
		if decided.Decision != nil {
			note = decided.Decision.Feedback
		}
		return AccessResult{Status: AccessDenied, ApprovalID: approval.ID, Reason: firstNonEmpty(note, "the person did not allow it")}, nil
	}
}

func (b *CredentialBroker) refuse(ctx context.Context, req AccessRequest, reason string) AccessResult {
	b.audit(ctx, req.TaskID, req.ExecutionID, store.CredentialAuditEntry{Event: "refuse", Credential: req.Credential, Operation: strings.Join(req.Operations, ","), Resource: strings.Join(req.Resources, " "), Detail: reason})
	return AccessResult{Status: AccessDenied, Reason: reason}
}

// covered reports whether live grants of the execution cover every pair.
func (b *CredentialBroker) covered(ctx context.Context, executionID, name string, ps []pair) bool {
	grants, err := b.db.LiveCredentialGrants(ctx, executionID, name, b.now())
	if err != nil {
		return false
	}
	for _, p := range ps {
		if findGrant(grants, p) == nil {
			return false
		}
	}
	return true
}

func findGrant(grants []store.CredentialGrant, p pair) *store.CredentialGrant {
	for i := range grants {
		var ops, resources []string
		_ = json.Unmarshal([]byte(grants[i].OperationsJSON), &ops)
		_ = json.Unmarshal([]byte(grants[i].ResourcesJSON), &resources)
		if !listHas(ops, p.operation) {
			continue
		}
		if len(resources) == 0 {
			resources = []string{""}
		}
		for _, r := range resources {
			if r == p.resource || (r != "" && credential.Covers(r, p.resource)) || (r == "" && p.resource == "") {
				return &grants[i]
			}
		}
	}
	return nil
}

func (b *CredentialBroker) grant(ctx context.Context, req AccessRequest, kind string, window time.Duration, approvalID, decidedBy string) error {
	ops, _ := json.Marshal(req.Operations)
	resources := req.Resources
	if resources == nil {
		resources = []string{}
	}
	res, _ := json.Marshal(resources)
	g := store.CredentialGrant{ID: newGrantID(), ExecutionID: req.ExecutionID, TaskID: req.TaskID, Credential: req.Credential,
		OperationsJSON: string(ops), ResourcesJSON: string(res), Mode: kind, ApprovalID: approvalID}
	switch kind {
	case grantWindow:
		expires := b.now().Add(window).UTC().Format(time.RFC3339Nano)
		g.ExpiresAt = &expires
	case grantOnce:
		one := 1
		g.UsesLeft = &one
	}
	if err := b.db.InsertCredentialGrant(ctx, g); err != nil {
		return err
	}
	detail := kind
	if kind == grantWindow {
		detail += " " + window.String()
	}
	b.audit(ctx, req.TaskID, req.ExecutionID, store.CredentialAuditEntry{Event: "grant", Credential: req.Credential, Operation: strings.Join(req.Operations, ","), Resource: strings.Join(req.Resources, " "), ApprovalID: approvalID, DecidedBy: decidedBy, Detail: detail})
	return nil
}

// credentialRequestDetails is what a credential approval's details hold.
type credentialRequestDetails struct {
	Credential    string   `json:"credential"`
	Profile       string   `json:"profile"`
	Operations    []string `json:"operations"`
	Resources     []string `json:"resources"`
	Reason        string   `json:"reason,omitempty"`
	Broader       bool     `json:"broader,omitempty"`
	Mode          string   `json:"mode"`
	WindowSeconds int      `json:"window_seconds,omitempty"`
	Signed        bool     `json:"signed,omitempty"`
	ScopeHash     string   `json:"scope_hash"`
	TaskTitle     string   `json:"task_title,omitempty"`
	AgentKind     string   `json:"agent_kind,omitempty"`
}

// submit asks the person, reusing a pending approval of the same scope.
func (b *CredentialBroker) submit(ctx context.Context, req AccessRequest, c brokerCredential, r config.CredentialRule) (*Approval, error) {
	scope := scopeHash(req)
	pending, err := b.approvals.List(ctx, store.ApprovalFilter{Status: store.ApprovalPending, ExecutionID: req.ExecutionID, Source: ApprovalSourceCredential})
	if err == nil {
		for i := range pending {
			var d credentialRequestDetails
			if json.Unmarshal(pending[i].Details, &d) == nil && d.ScopeHash == scope {
				return &pending[i], nil
			}
		}
	}
	window := r.WindowDuration
	if window <= 0 {
		window = defaultWindow
	}
	broader := false
	for _, resource := range req.Resources {
		broader = broader || credential.IsPattern(resource)
	}
	details := credentialRequestDetails{
		Credential: req.Credential, Profile: c.profile.Name, Operations: req.Operations, Resources: req.Resources,
		Reason: clip(req.Reason, 500), Broader: broader, Mode: r.Approve, WindowSeconds: int(window.Seconds()), Signed: r.Signed, ScopeHash: scope,
	}
	if task, err := b.db.GetTask(ctx, req.TaskID); err == nil {
		details.TaskTitle = task.Title
	}
	if execution, err := b.db.GetExecution(ctx, req.ExecutionID); err == nil {
		details.AgentKind = execution.AgentKind
	}
	approval, _, err := b.approvals.Submit(ctx, ApprovalInput{
		Source: ApprovalSourceCredential, SourceKey: req.ExecutionID + "/" + scope + "/" + strconv.FormatInt(b.now().UnixNano(), 36),
		TaskID: req.TaskID, ExecutionID: req.ExecutionID,
		Title:   "Use " + req.Credential,
		Summary: strings.Join(req.Operations, ", ") + " on " + orAll(strings.Join(req.Resources, ", ")),
		Details: details, Options: grantOptions(r.Approve, window), Blocking: req.Blocking,
	})
	return approval, err
}

// grantOptions are the rule's mode and every stricter one, then deny.
func grantOptions(mode string, window time.Duration) []ApprovalOption {
	var options []ApprovalOption
	switch mode {
	case "session":
		options = append(options, ApprovalOption{ID: grantSession, Label: "For this session", Kind: ApprovalAllow})
		fallthrough
	case "window":
		options = append(options, ApprovalOption{ID: grantWindow, Label: "For " + humanDuration(window), Kind: ApprovalAllow})
		fallthrough
	case "request":
		options = append(options, ApprovalOption{ID: grantOnce, Label: "Once", Kind: ApprovalAllow})
	}
	return append(options, ApprovalOption{ID: "deny", Label: "Deny", Kind: ApprovalDeny})
}

// Authorize checks one use against the rules and the execution's grants. A
// use no grant covers becomes a request for exactly that use.
func (b *CredentialBroker) Authorize(ctx context.Context, use CredentialUse) (UseDecision, error) {
	c, ok := b.lookup(ctx, use.Credential)
	if !ok {
		return UseDecision{}, fmt.Errorf("no credential named %q", use.Credential)
	}
	entry := store.CredentialAuditEntry{Credential: use.Credential, Operation: use.Operation, Resource: use.Resource, Amount: amounts(use.Amounts)}
	if !b.executionRunning(ctx, use.ExecutionID, use.TaskID) {
		entry.Event, entry.Detail = "refuse", withCall("the agent is not running", use.Call)
		b.audit(ctx, use.TaskID, use.ExecutionID, entry)
		return UseDecision{Reason: entry.Detail}, nil
	}
	p := pair{use.Operation, use.Resource}
	r := b.selectRule(ctx, c, p, use.Amounts)
	if r == nil || r.Approve == "deny" {
		reason := noRuleReason(c, use.Operation, use.Resource)
		if r != nil {
			reason = fmt.Sprintf("%s does not allow %s on %s", use.Credential, use.Operation, orAll(use.Resource))
		}
		entry.Event, entry.Detail = "refuse", withCall(reason, use.Call)
		b.audit(ctx, use.TaskID, use.ExecutionID, entry)
		return UseDecision{Reason: reason}, nil
	}
	if r.Approve == "auto" {
		entry.Event, entry.Detail = "use", withCall("auto", use.Call)
		b.audit(ctx, use.TaskID, use.ExecutionID, entry)
		b.meter(ctx, use.Credential, use.Amounts)
		return UseDecision{Allowed: true}, nil
	}
	if ok, err := b.useGrant(ctx, use, entry); ok || err != nil {
		return UseDecision{Allowed: ok}, err
	}
	result, err := b.Request(ctx, AccessRequest{TaskID: use.TaskID, ExecutionID: use.ExecutionID, Credential: use.Credential,
		Operations: []string{use.Operation}, Resources: nonEmpty(use.Resource), Reason: "used without asking first", Blocking: true, amounts: use.Amounts})
	if err != nil {
		return UseDecision{}, err
	}
	if result.Status == AccessGranted {
		if ok, err := b.useGrant(ctx, use, entry); ok || err != nil {
			return UseDecision{Allowed: ok, ApprovalID: result.ApprovalID}, err
		}
	}
	return UseDecision{ApprovalID: result.ApprovalID, Reason: firstNonEmpty(result.Reason, "waiting for the person")}, nil
}

// CheckUse decides a use as Authorize would, without spending a grant or
// recording a use: for a channel that must decide several uses together
// (the refs of a push) before it spends any. A use no grant covers asks the
// person without waiting. A refusal is recorded, as it is final.
func (b *CredentialBroker) CheckUse(ctx context.Context, use CredentialUse) (UseDecision, error) {
	c, ok := b.lookup(ctx, use.Credential)
	if !ok {
		return UseDecision{}, fmt.Errorf("no credential named %q", use.Credential)
	}
	entry := store.CredentialAuditEntry{Event: "refuse", Credential: use.Credential, Operation: use.Operation, Resource: use.Resource, Amount: amounts(use.Amounts)}
	if !b.executionRunning(ctx, use.ExecutionID, use.TaskID) {
		entry.Detail = withCall("the agent is not running", use.Call)
		b.audit(ctx, use.TaskID, use.ExecutionID, entry)
		return UseDecision{Reason: "the agent is not running"}, nil
	}
	r := b.selectRule(ctx, c, pair{use.Operation, use.Resource}, use.Amounts)
	if r == nil || r.Approve == "deny" {
		reason := noRuleReason(c, use.Operation, use.Resource)
		if r != nil {
			reason = fmt.Sprintf("%s does not allow %s on %s", use.Credential, use.Operation, orAll(use.Resource))
		}
		entry.Detail = withCall(reason, use.Call)
		b.audit(ctx, use.TaskID, use.ExecutionID, entry)
		return UseDecision{Reason: reason}, nil
	}
	if r.Approve == "auto" {
		return UseDecision{Allowed: true}, nil
	}
	grants, err := b.db.LiveCredentialGrants(ctx, use.ExecutionID, use.Credential, b.now())
	if err != nil {
		return UseDecision{}, err
	}
	if findGrant(personGrants(grants), pair{use.Operation, use.Resource}) != nil {
		return UseDecision{Allowed: true}, nil
	}
	result, err := b.Request(ctx, AccessRequest{TaskID: use.TaskID, ExecutionID: use.ExecutionID, Credential: use.Credential,
		Operations: []string{use.Operation}, Resources: nonEmpty(use.Resource), Reason: "used without asking first", Blocking: true, amounts: use.Amounts, noWait: true})
	if err != nil {
		return UseDecision{}, err
	}
	if result.Status == AccessGranted {
		return UseDecision{Allowed: true, ApprovalID: result.ApprovalID}, nil
	}
	return UseDecision{ApprovalID: result.ApprovalID, Reason: firstNonEmpty(result.Reason, "waiting for the person")}, nil
}

// useGrant spends a live grant that covers the use, if there is one.
func (b *CredentialBroker) useGrant(ctx context.Context, use CredentialUse, entry store.CredentialAuditEntry) (bool, error) {
	grants, err := b.db.LiveCredentialGrants(ctx, use.ExecutionID, use.Credential, b.now())
	if err != nil {
		return false, err
	}
	g := findGrant(personGrants(grants), pair{use.Operation, use.Resource})
	if g == nil {
		return false, nil
	}
	if g.UsesLeft != nil {
		if err := b.db.UseCredentialGrant(ctx, g.ID); errors.Is(err, store.ErrConflict) {
			return false, nil
		} else if err != nil {
			return false, err
		}
	}
	entry.Event, entry.ApprovalID, entry.Detail = "use", g.ApprovalID, withCall(g.Mode, use.Call)
	b.audit(ctx, use.TaskID, use.ExecutionID, entry)
	b.meter(ctx, use.Credential, use.Amounts)
	return true, nil
}

// personGrants are the grants a person made, not those an auto rule left.
func personGrants(grants []store.CredentialGrant) []store.CredentialGrant {
	out := grants[:0:0]
	for _, g := range grants {
		if g.Mode != grantAuto {
			out = append(out, g)
		}
	}
	return out
}

// noRuleReason says why no rule allows a use: for a credential that lists
// its repositories, that the repository is not one of them.
func noRuleReason(c brokerCredential, operation, resource string) string {
	if repos := c.config.Settings.Repositories; len(repos) > 0 {
		if repo, err := repoOf(resource); err == nil && !repoListed(repos, repo) {
			return fmt.Sprintf("%s is not one of the repositories of %s (settings.repositories in config.yaml)", repo, c.config.Name)
		}
	}
	return fmt.Sprintf("no rule of %s allows %s on %s", c.config.Name, operation, orAll(resource))
}

// repoListed reports whether owner/name is in a list of repositories, where
// owner/* stands for every repository of that owner.
func repoListed(list []string, repo string) bool {
	for _, entry := range list {
		if entry == repo || credential.Covers("github:"+entry, "github:"+repo) {
			return true
		}
	}
	return false
}

// withCall adds what the channel saw to an audit detail.
func withCall(detail, call string) string {
	if call == "" {
		return detail
	}
	return detail + " · " + clip(call, 300)
}

// readSecret reads a credential's secret from its store, with no use: for a
// channel that has authorized the use itself, or needs a public key.
func (b *CredentialBroker) readSecret(ctx context.Context, name string) (credential.Secret, error) {
	c, ok := b.lookup(ctx, name)
	if !ok {
		return credential.Secret{}, fmt.Errorf("no credential named %q", name)
	}
	s, ok := b.stores[c.ref.Scheme]
	if !ok {
		return credential.Secret{}, fmt.Errorf("no store for %s:// references in this Legato", c.ref.Scheme)
	}
	secret, err := s.Get(ctx, c.ref)
	if err != nil {
		return credential.Secret{}, err
	}
	if secret.Type == "" || c.config.Type != "" {
		secret.Type = firstNonEmpty(c.config.Type, c.profile.defaultType(), credential.TypeValue)
	}
	return secret, nil
}

// Value is a use that also reads the credential, through its issuer.
func (b *CredentialBroker) Value(ctx context.Context, use CredentialUse) (credential.Secret, UseDecision, error) {
	decision, err := b.Authorize(ctx, use)
	if err != nil || !decision.Allowed {
		return credential.Secret{}, decision, err
	}
	c, _ := b.lookup(ctx, use.Credential)
	s, ok := b.stores[c.ref.Scheme]
	if !ok {
		return credential.Secret{}, decision, fmt.Errorf("no store for %s:// references in this Legato", c.ref.Scheme)
	}
	secret, err := s.Get(ctx, c.ref)
	if err != nil {
		return credential.Secret{}, decision, err
	}
	if secret.Type == "" || c.config.Type != "" {
		secret.Type = firstNonEmpty(c.config.Type, credential.TypeValue)
	}
	issued, err := credential.StaticIssuer{}.Issue(ctx, secret, credential.Scope{Operations: []string{use.Operation}, Resources: nonEmpty(use.Resource)})
	if err != nil {
		return credential.Secret{}, decision, err
	}
	return credential.Secret{Type: secret.Type, Value: issued.Value}, decision, nil
}

func (b *CredentialBroker) audit(ctx context.Context, taskID, executionID string, e store.CredentialAuditEntry) {
	e.TaskID, e.ExecutionID = taskID, executionID
	if execution, err := b.db.GetExecution(ctx, executionID); err == nil {
		e.AgentKind = execution.AgentKind
	}
	if task, err := b.db.GetTask(ctx, taskID); err == nil && task.WorkspaceID != nil {
		e.WorkspaceID = strconv.Itoa(*task.WorkspaceID)
	}
	if _, err := b.db.AppendCredentialAudit(ctx, e); err != nil {
		log.Printf("credentials: audit: %v", err)
	}
}

// credentialResolver turns decisions on credential approvals into grants,
// and tells the agent when it no longer waits for the answer.
type credentialResolver struct{ b *CredentialBroker }

func (r credentialResolver) Resolve(ctx context.Context, approval Approval, decision ApprovalDecision) error {
	var d credentialRequestDetails
	if err := json.Unmarshal(approval.Details, &d); err != nil {
		return err
	}
	req := AccessRequest{TaskID: approval.TaskID, ExecutionID: approval.ExecutionID, Credential: d.Credential, Operations: d.Operations, Resources: d.Resources}
	scope := strings.Join(d.Operations, ", ") + " on " + orAll(strings.Join(d.Resources, ", "))
	var message string
	switch decision.Decision {
	case ApprovalAllow:
		window := time.Duration(d.WindowSeconds) * time.Second
		if err := r.b.grant(ctx, req, decision.Option, window, approval.ID, decision.By); err != nil {
			return err
		}
		phrase := map[string]string{grantSession: "for this session", grantWindow: "for " + humanDuration(window), grantOnce: "once"}[decision.Option]
		message = fmt.Sprintf("[legato approvals] %s approved: %s with %s %s. Carry on with what needed it.", approval.ID, scope, d.Credential, phrase)
	default:
		r.b.audit(ctx, approval.TaskID, approval.ExecutionID, store.CredentialAuditEntry{Event: "deny", Credential: d.Credential, Operation: strings.Join(d.Operations, ","), Resource: strings.Join(d.Resources, " "), ApprovalID: approval.ID, DecidedBy: decision.By, Detail: clip(decision.Feedback, 500)})
		note := firstNonEmpty(decision.Feedback, "no reason given")
		message = fmt.Sprintf("[legato approvals] %s denied: %s with %s. The person said: %s. Do not look for another way to get this credential.", approval.ID, scope, d.Credential, note)
	}
	// A decision made while the requester still waited reached it directly.
	if r.b.messenger != nil && r.b.now().Sub(parseApprovalTime(approval.CreatedAt)) >= r.b.wait {
		if err := r.b.messenger.QueueMessage(ctx, approval.TaskID, message); err != nil {
			log.Printf("credentials: telling %s of %s: %v", approval.TaskID, approval.ID, err)
		}
	}
	return nil
}

func parseApprovalTime(s string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.000Z", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func grantPhrase(approval *Approval) string {
	if approval.Decision == nil {
		return ""
	}
	switch approval.Decision.Option {
	case grantSession:
		return "this session"
	case grantWindow:
		var d credentialRequestDetails
		_ = json.Unmarshal(approval.Details, &d)
		return humanDuration(time.Duration(d.WindowSeconds) * time.Second)
	case grantOnce:
		return "once"
	}
	return approval.Decision.Option
}

func scopeHash(req AccessRequest) string {
	ops := append([]string{}, req.Operations...)
	resources := append([]string{}, req.Resources...)
	sort.Strings(ops)
	sort.Strings(resources)
	sum := sha256.Sum256([]byte(req.Credential + "\x00" + strings.Join(ops, "\x00") + "\x00\x00" + strings.Join(resources, "\x00")))
	return hex.EncodeToString(sum[:8])
}

func newGrantID() string {
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	return "cg-" + hex.EncodeToString(b)
}

func humanDuration(d time.Duration) string {
	switch {
	case d >= time.Hour && d%time.Hour == 0:
		if d == time.Hour {
			return "1 hour"
		}
		return fmt.Sprintf("%d hours", d/time.Hour)
	case d >= time.Minute && d%time.Minute == 0:
		return fmt.Sprintf("%d minutes", d/time.Minute)
	}
	return d.String()
}

func amounts(m map[string]int64) string {
	if len(m) == 0 {
		return ""
	}
	data, _ := json.Marshal(m)
	return string(data)
}

func orAll(resource string) string {
	if resource == "" {
		return "all of it"
	}
	return resource
}

func nonEmpty(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}

func listHas(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// GrantInfo is a live grant, as the person sees it.
type GrantInfo struct {
	ID          string   `json:"id"`
	Credential  string   `json:"credential"`
	TaskID      string   `json:"task_id"`
	ExecutionID string   `json:"execution_id"`
	Operations  []string `json:"operations"`
	Resources   []string `json:"resources"`
	Mode        string   `json:"mode"`
	UsesLeft    *int     `json:"uses_left,omitempty"`
	ExpiresAt   *string  `json:"expires_at,omitempty"`
	ApprovalID  string   `json:"approval_id,omitempty"`
	CreatedAt   string   `json:"created_at"`
}

// Grants lists every live grant.
func (b *CredentialBroker) Grants(ctx context.Context) ([]GrantInfo, error) {
	b.endGrantsOfEndedAgents(ctx)
	rows, err := b.db.ListLiveCredentialGrants(ctx, b.now())
	if err != nil {
		return nil, err
	}
	out := make([]GrantInfo, 0, len(rows))
	for _, g := range rows {
		info := GrantInfo{ID: g.ID, Credential: g.Credential, TaskID: g.TaskID, ExecutionID: g.ExecutionID, Mode: g.Mode,
			UsesLeft: g.UsesLeft, ExpiresAt: g.ExpiresAt, ApprovalID: g.ApprovalID, CreatedAt: g.CreatedAt, Operations: []string{}, Resources: []string{}}
		_ = json.Unmarshal([]byte(g.OperationsJSON), &info.Operations)
		_ = json.Unmarshal([]byte(g.ResourcesJSON), &info.Resources)
		out = append(out, info)
	}
	return out, nil
}

// EndGrant ends a grant before its time.
func (b *CredentialBroker) EndGrant(ctx context.Context, id, by string) error {
	grants, err := b.Grants(ctx)
	if err != nil {
		return err
	}
	for _, g := range grants {
		if g.ID != id {
			continue
		}
		if err := b.db.EndCredentialGrant(ctx, id); err != nil {
			return err
		}
		b.audit(ctx, g.TaskID, g.ExecutionID, store.CredentialAuditEntry{Event: "revoke", Credential: g.Credential, Operation: strings.Join(g.Operations, ","), Resource: strings.Join(g.Resources, " "), ApprovalID: g.ApprovalID, DecidedBy: by})
		return nil
	}
	return store.ErrNotFound
}

// Audit returns audit rows, newest first.
func (b *CredentialBroker) Audit(ctx context.Context, filter store.CredentialAuditFilter) ([]store.CredentialAuditEntry, error) {
	return b.db.ListCredentialAudit(ctx, filter)
}
