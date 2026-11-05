package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/credential"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/pairing"
	"github.com/cpave3/legato/internal/engine/store"
)

// HandoffProtocolVersion is the version of the calls a main instance and a
// worker make to each other. A worker advertises it in /api/meta; a main
// pairs with and hands off to a worker only when it is at least this.
// Version 2 adds the Handoff call (POST /api/integrations/handoff/tasks).
const HandoffProtocolVersion = 2

var (
	// ErrWorkerInput: the address, phrase or name given to Add a worker
	// cannot be used.
	ErrWorkerInput = errors.New("cannot pair with that worker")
	// ErrWorkerTooOld: the worker cannot pair a main instance or speak this
	// main's handoff protocol.
	ErrWorkerTooOld = errors.New("update the worker: it cannot pair a main instance")
	// ErrWorkerUnreachable: the worker did not answer.
	ErrWorkerUnreachable = errors.New("the worker did not answer")
	// ErrWorkerRefused: the worker answered, but did not pair: a wrong
	// phrase, no open request, a name another main has, or a secret this
	// main will not take.
	ErrWorkerRefused = errors.New("the worker did not pair")
	// ErrWorkerNotPaired: no worker of that instance ID is paired.
	ErrWorkerNotPaired = errors.New("that worker is not paired")
)

// Worker is a worker instance this main paired with, as Settings › Workers
// lists it. MainName is this main's name on the worker.
type Worker struct {
	InstanceID     string  `json:"instance_id"`
	Address        string  `json:"address"`
	Name           string  `json:"name"`
	MainName       string  `json:"main_name"`
	Sandbox        bool    `json:"sandbox"`
	HandoffVersion int     `json:"handoff_version"`
	PairedAt       string  `json:"paired_at"`
	// LastPullAt is when this main last heard from the worker: a pull, a
	// summary row or a pong on its connection.
	LastPullAt *string `json:"last_pull_at,omitempty"`
	// UnreachableSince is when the worker went down, once it has been down
	// longer than the grace; RefusedAt is when it stopped accepting this
	// main's token (pair again).
	UnreachableSince *string `json:"unreachable_since,omitempty"`
	RefusedAt        *string `json:"refused_at,omitempty"`
}

// WorkerPairRequest is Add a worker: the worker's address (scheme optional,
// https assumed), the four words it shows, and this main's name on it.
type WorkerPairRequest struct {
	Address string `json:"address"`
	Phrase  string `json:"phrase"`
	Name    string `json:"name"`
}

// WorkerDefaults are what Add a worker starts from: this main's instance ID
// and the name it suggests for it.
type WorkerDefaults struct {
	InstanceID string `json:"instance_id"`
	MainName   string `json:"main_name"`
}

// WorkerSecrets is where a worker's token is kept: Legato's sealed secret
// store (SecretStoreService).
type WorkerSecrets interface {
	Add(ctx context.Context, name, secretType string, value []byte) error
	Get(ctx context.Context, ref credential.Ref) (credential.Secret, error)
}

// WorkerServiceOptions configure a main instance's workers.
type WorkerServiceOptions struct {
	// Secrets keeps each worker's token sealed.
	Secrets WorkerSecrets
	// MainName is the name this main suggests for itself on a worker,
	// normalised (usually the host name).
	MainName string
	// Now is the clock; time.Now when nil.
	Now func() time.Time
	// Timeout bounds each call to a worker; 10 seconds when zero.
	Timeout time.Duration
	// Bus hears that a worker was marked unreachable (MarkUnreachable);
	// nil for none.
	Bus *events.Bus
}

// WorkerService is a main instance's side of pairing with workers: Add a
// worker (Pair), the list, and a client for each worker that later calls use.
type WorkerService struct {
	db   *store.Store
	opts WorkerServiceOptions
}

func NewWorkerService(db *store.Store, opts WorkerServiceOptions) *WorkerService {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Timeout == 0 {
		opts.Timeout = 10 * time.Second
	}
	return &WorkerService{db: db, opts: opts}
}

// Defaults are this main's instance ID and the name it suggests for itself.
func (s *WorkerService) Defaults(ctx context.Context) (WorkerDefaults, error) {
	id, err := InstanceID(ctx, s.db)
	if err != nil {
		return WorkerDefaults{}, err
	}
	name, err := NormaliseMainName(s.opts.MainName)
	if err != nil {
		name = ""
	}
	return WorkerDefaults{InstanceID: id, MainName: name}, nil
}

// List lists the paired workers.
func (s *WorkerService) List(ctx context.Context) ([]Worker, error) {
	rows, err := s.db.ListWorkers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Worker, 0, len(rows))
	for _, w := range rows {
		out = append(out, workerFromRow(w))
	}
	return out, nil
}

func workerFromRow(w store.Worker) Worker {
	return Worker{InstanceID: w.InstanceID, Address: w.Address, Name: w.Name, MainName: w.MainName, Sandbox: w.Sandbox,
		HandoffVersion: w.HandoffVersion, PairedAt: w.PairedAt, LastPullAt: w.LastPullAt, UnreachableSince: w.UnreachableSince, RefusedAt: w.RefusedAt}
}

// workerMeta is what a main reads of a worker's /api/meta.
type workerMeta struct {
	Version        string `json:"version"`
	HandoffPairing int    `json:"handoff_pairing"`
	Handoff        int    `json:"handoff"`
	Sandbox        *bool  `json:"sandbox"`
}

// Pair pairs this main with a worker by the phrase the worker shows: it
// checks the worker can pair a main, runs the exchange as the main (its own
// SPAKE2 identity, its name and instance ID sealed), refuses a secret whose
// token is not a scoped lgi_ token, checks the token reaches the worker over
// the worker's certificate, and keeps the worker with its token sealed.
// Pairing the same worker again replaces its record.
func (s *WorkerService) Pair(ctx context.Context, req WorkerPairRequest) (Worker, error) {
	address, err := normaliseWorkerAddress(req.Address)
	if err != nil {
		return Worker{}, err
	}
	words, err := parsePhrase(req.Phrase)
	if err != nil {
		return Worker{}, err
	}
	name, err := NormaliseMainName(req.Name)
	if err != nil {
		return Worker{}, fmt.Errorf("%w: this main's name: %v", ErrWorkerInput, err)
	}
	mainID, err := InstanceID(ctx, s.db)
	if err != nil {
		return Worker{}, err
	}
	if s.opts.Secrets == nil {
		return Worker{}, errors.New("this Legato has no secret store to keep a worker's token in")
	}

	// Before the exchange the main does not know the worker's certificate.
	// It does not need to: the phrase authenticates the exchange, and the
	// certificate to trust from then on comes sealed in its secret.
	unpinned := s.httpClient(&tls.Config{InsecureSkipVerify: true}) //nolint:gosec // the phrase authenticates the exchange
	var meta workerMeta
	if err := workerCall(ctx, unpinned, http.MethodGet, address+"/api/meta", "", nil, &meta); err != nil {
		return Worker{}, err
	}
	if meta.HandoffPairing < 1 || meta.Handoff < HandoffProtocolVersion {
		return Worker{}, fmt.Errorf("%w (%s runs Legato %s)", ErrWorkerTooOld, address, orUnknown(meta.Version))
	}

	secret, err := s.exchange(ctx, unpinned, address, words, name, mainID)
	if err != nil {
		return Worker{}, err
	}
	if !strings.HasPrefix(secret.Token, IntegrationTokenPrefix) {
		return Worker{}, fmt.Errorf("%w: it sent a token that is not a handoff token; update the worker", ErrWorkerRefused)
	}
	if !ValidInstanceID(secret.InstanceID) {
		return Worker{}, fmt.Errorf("%w: it sent no instance ID; update the worker", ErrWorkerRefused)
	}

	row := store.Worker{
		InstanceID: secret.InstanceID, Address: address, Name: secret.Name, MainName: name,
		CACertificate: secret.CACertificate, TokenSecret: workerTokenSecret(secret.InstanceID),
		Sandbox: meta.Sandbox != nil && *meta.Sandbox, HandoffVersion: meta.Handoff,
		PairedAt: s.opts.Now().UTC().Format(time.RFC3339),
	}
	client, err := s.clientFor(row, secret.Token)
	if err != nil {
		return Worker{}, err
	}
	if err := client.CheckPairing(ctx); err != nil {
		return Worker{}, fmt.Errorf("paired, but this main cannot use the pairing: %w", err)
	}
	if err := s.opts.Secrets.Add(ctx, row.TokenSecret, credential.TypeValue, []byte(secret.Token)); err != nil {
		return Worker{}, fmt.Errorf("keeping the worker's token: %w", err)
	}
	if err := s.db.SaveWorker(ctx, row); err != nil {
		return Worker{}, err
	}
	saved, err := s.db.GetWorker(ctx, row.InstanceID)
	if err != nil {
		return Worker{}, err
	}
	return workerFromRow(*saved), nil
}

// exchange runs SPAKE2 as the main, party A, against the worker's open
// request, and opens the secret it seals.
func (s *WorkerService) exchange(ctx context.Context, client *http.Client, address string, words []string, name, mainID string) (PairingSecret, error) {
	b64 := base64.StdEncoding
	session, share, err := pairing.Start(pairing.PartyA, pairing.MainProtocol, pairing.PhraseScalar(words), rand.Reader)
	if err != nil {
		return PairingSecret{}, err
	}
	var exchanged struct {
		Session string `json:"session"`
		Share   string `json:"share"`
	}
	if err := workerCall(ctx, client, http.MethodPost, address+"/api/pair/exchange", "", map[string]string{"share": b64.EncodeToString(share)}, &exchanged); err != nil {
		return PairingSecret{}, err
	}
	workerShare, err := b64.DecodeString(exchanged.Share)
	if err != nil {
		return PairingSecret{}, fmt.Errorf("%w: its answer is not valid", ErrWorkerRefused)
	}
	keys, err := session.Finish(workerShare)
	if err != nil {
		return PairingSecret{}, fmt.Errorf("%w: its answer is not valid", ErrWorkerRefused)
	}
	description, err := json.Marshal(map[string]string{"name": name, "instance_id": mainID})
	if err != nil {
		return PairingSecret{}, err
	}
	sealedDescription, err := pairing.Seal(keys.Key, pairing.LabelMain, description, rand.Reader)
	if err != nil {
		return PairingSecret{}, err
	}
	var confirmed struct {
		Confirmation string `json:"confirmation"`
		Sealed       string `json:"sealed"`
	}
	if err := workerCall(ctx, client, http.MethodPost, address+"/api/pair/confirm", "", map[string]string{
		"session":      exchanged.Session,
		"confirmation": b64.EncodeToString(keys.Confirmation),
		"phone":        b64.EncodeToString(sealedDescription),
	}, &confirmed); err != nil {
		return PairingSecret{}, err
	}
	confirmation, err := b64.DecodeString(confirmed.Confirmation)
	if err != nil || !keys.Confirms(confirmation) {
		return PairingSecret{}, fmt.Errorf("%w: it did not prove it knows the phrase", ErrWorkerRefused)
	}
	sealed, err := b64.DecodeString(confirmed.Sealed)
	if err != nil {
		return PairingSecret{}, fmt.Errorf("%w: its secret is not valid", ErrWorkerRefused)
	}
	plain, err := pairing.Open(keys.Key, pairing.LabelServer, sealed)
	if err != nil {
		return PairingSecret{}, fmt.Errorf("%w: its secret does not open", ErrWorkerRefused)
	}
	var secret PairingSecret
	if err := json.Unmarshal(plain, &secret); err != nil {
		return PairingSecret{}, fmt.Errorf("%w: its secret is not valid", ErrWorkerRefused)
	}
	return secret, nil
}

// WorkerClient calls one paired worker: its address, over its certificate,
// with this main's handoff token. Later handoff calls (handoff, pull,
// events, return) are made through it.
type WorkerClient struct {
	Worker Worker
	base   string
	token  string
	http   *http.Client
}

// Client is the client of a paired worker, or ErrWorkerNotPaired.
func (s *WorkerService) Client(ctx context.Context, instanceID string) (*WorkerClient, error) {
	row, err := s.db.GetWorker(ctx, instanceID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("%w: %s", ErrWorkerNotPaired, instanceID)
	}
	if err != nil {
		return nil, err
	}
	if s.opts.Secrets == nil {
		return nil, errors.New("this Legato has no secret store")
	}
	secret, err := s.opts.Secrets.Get(ctx, credential.Ref{Scheme: "legato", Path: row.TokenSecret})
	if err != nil {
		return nil, fmt.Errorf("reading the token of worker %s: %w", row.Name, err)
	}
	return s.clientFor(*row, string(secret.Value))
}

func (s *WorkerService) clientFor(row store.Worker, token string) (*WorkerClient, error) {
	cfg := &tls.Config{}
	if row.CACertificate != "" {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(row.CACertificate)) {
			return nil, fmt.Errorf("%w: its certificate is not valid", ErrWorkerRefused)
		}
		cfg.RootCAs = pool
	}
	return &WorkerClient{Worker: workerFromRow(row), base: row.Address, token: token, http: s.httpClient(cfg)}, nil
}

func (s *WorkerService) httpClient(cfg *tls.Config) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = cfg
	return &http.Client{Transport: transport, Timeout: s.opts.Timeout}
}

// Call sends a JSON body (nil for none) to path on the worker with this
// main's token and decodes the JSON answer into into (nil to drop it). A
// refusal is an error with the worker's message: ErrWorkerRefused, or
// ErrWorkerUnreachable when there was no answer.
func (c *WorkerClient) Call(ctx context.Context, method, path string, body, into any) error {
	return workerCall(ctx, c.http, method, c.base+path, c.token, body, into)
}

// WorkerRefusal is a worker that answered a call with a refusal: Message
// is the worker's own words. errors.Is matches ErrWorkerRefused.
type WorkerRefusal struct {
	Message string
	// Status is the HTTP status it answered with: 401 when it no longer
	// takes this main's token.
	Status int
	// Code is the integration route's error code (not_found, bad_request,
	// ...), when it gave one.
	Code string
}

func (e *WorkerRefusal) Error() string        { return ErrWorkerRefused.Error() + ": " + e.Message }
func (e *WorkerRefusal) Is(target error) bool { return target == ErrWorkerRefused }

// WorkerMeta is what a worker's /api/meta says of it: its version, whether
// it pairs mains, the handoff protocol it speaks and whether its agents can
// run in Legato's sandbox.
type WorkerMeta = workerMeta

// Meta reads the worker's /api/meta over its certificate. It is asked
// without the main's token, which reaches only the handoff routes.
func (c *WorkerClient) Meta(ctx context.Context) (WorkerMeta, error) {
	var meta workerMeta
	err := workerCall(ctx, c.http, http.MethodGet, c.base+"/api/meta", "", nil, &meta)
	return meta, err
}

// CheckPairing asks the worker how it sees this pairing, and checks it is
// the worker this main paired with and that it knows this main.
func (c *WorkerClient) CheckPairing(ctx context.Context) error {
	var seen struct {
		WorkerInstanceID string `json:"worker_instance_id"`
		MainInstanceID   string `json:"main_instance_id"`
	}
	if err := c.Call(ctx, http.MethodGet, "/api/integrations/handoff/pairing", nil, &seen); err != nil {
		return err
	}
	if seen.WorkerInstanceID != c.Worker.InstanceID {
		return fmt.Errorf("%w: it answers as %s, not %s", ErrWorkerRefused, orUnknown(seen.WorkerInstanceID), c.Worker.InstanceID)
	}
	return nil
}

// workerCall makes one call to a worker and decodes its answer.
func workerCall(ctx context.Context, client *http.Client, method, address, token string, body, into any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, address, reader)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrWorkerInput, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrWorkerUnreachable, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrWorkerUnreachable, err)
	}
	if resp.StatusCode/100 != 2 {
		// Pairing routes answer {"error": text}; integration routes
		// {"error": code, "message": text}.
		var failure struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &failure)
		message, code := failure.Message, ""
		if message == "" {
			message = failure.Error
		} else {
			code = failure.Error
		}
		if message == "" {
			message = resp.Status
		}
		if resp.StatusCode == http.StatusNotFound && strings.HasSuffix(address, "/api/pair/exchange") {
			return fmt.Errorf("%w: it does not pair by phrase", ErrWorkerTooOld)
		}
		return &WorkerRefusal{Message: message, Status: resp.StatusCode, Code: code}
	}
	if into == nil {
		return nil
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("%w: its answer is not JSON", ErrWorkerRefused)
	}
	return nil
}

// workerTokenSecret is the name a worker's token is sealed under in
// Legato's secret store.
func workerTokenSecret(instanceID string) string { return "handoff-worker-" + instanceID }

// normaliseWorkerAddress makes what was typed an address to call: https when
// no scheme is given, no trailing slash, no path.
func normaliseWorkerAddress(raw string) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		return "", fmt.Errorf("%w: give the worker's address", ErrWorkerInput)
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || (u.Path != "" && u.Path != "/") || u.RawQuery != "" {
		return "", fmt.Errorf("%w: %q is not an address like https://vm-1.lan:3080", ErrWorkerInput, raw)
	}
	return u.Scheme + "://" + u.Host, nil
}

var phraseSeparators = regexp.MustCompile(`[^a-z]+`)

// parsePhrase reads the four words a worker shows, however they were
// separated: spaces, dashes or dots.
func parsePhrase(raw string) ([]string, error) {
	words := strings.Fields(phraseSeparators.ReplaceAllString(strings.ToLower(raw), " "))
	if len(words) != 4 {
		return nil, fmt.Errorf("%w: the phrase is the four words the worker shows", ErrWorkerInput)
	}
	known := pairing.Words()
	for _, w := range words {
		if !slices.Contains(known, w) {
			return nil, fmt.Errorf("%w: %q is not a word of a pairing phrase", ErrWorkerInput, w)
		}
	}
	return words, nil
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}
