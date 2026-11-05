package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/engine/pairing"
)

// A pairing request lets one phone, or one main instance, pair by a
// four-word phrase. It is open for PairingLifetime, allows PairingAttempts
// tries, and ends when one pairs.
const (
	PairingLifetime = 5 * time.Minute
	PairingAttempts = 5
)

// PairingState is where a pairing request stands.
type PairingState string

const (
	PairingNone      PairingState = "none"
	PairingOpen      PairingState = "open"
	PairingPaired    PairingState = "paired"
	PairingExpired   PairingState = "expired"
	PairingExhausted PairingState = "exhausted"
	PairingCancelled PairingState = "cancelled"
)

// PairingPurpose is what a request pairs: a phone, which gets the full web
// token, or a main instance, which gets a token that can only hand off.
type PairingPurpose string

const (
	PairingPhone PairingPurpose = "phone"
	PairingMain  PairingPurpose = "main"
)

var (
	// ErrPairingPurpose: a request for something this server does not pair.
	ErrPairingPurpose = errors.New("this server does not pair that")
	// ErrPairingClient: a main instance did not say, sealed, a usable name
	// and instance ID.
	ErrPairingClient = errors.New("the main instance must give its name and instance ID")
	// ErrPairingClosed: no request is open (none, used, expired, out of tries, cancelled).
	ErrPairingClosed = errors.New("no pairing request is open")
	// ErrPairingMismatch: the phone's phrase is not the request's.
	ErrPairingMismatch = errors.New("the phrase does not match")
	// ErrPairingBadShare: what the phone sent is not part of an exchange.
	ErrPairingBadShare = errors.New("the pairing message is not valid")
)

// PairingStatus is what the person who opened a request is shown. Device is
// the phone's name, or the main instance's, once one paired.
type PairingStatus struct {
	State        PairingState   `json:"state"`
	Purpose      PairingPurpose `json:"purpose,omitempty"`
	Phrase       []string       `json:"phrase,omitempty"`
	ExpiresAt    time.Time      `json:"expires_at,omitzero"`
	AttemptsLeft int            `json:"attempts_left"`
	Device       string         `json:"device,omitempty"`
}

// PairingSecret is what a paired phone or main instance receives, sealed:
// the token, this server's name and the certificate to trust. A main also
// gets this server's instance ID; a phone's secret never has one.
type PairingSecret struct {
	Token         string `json:"token"`
	Name          string `json:"name"`
	CACertificate string `json:"ca_certificate,omitempty"`
	InstanceID    string `json:"instance_id,omitempty"`
}

// PairingClient is who completed a request, as the secret callback is told:
// a phone with the name it gave (perhaps none), or a main instance with its
// normalised name and its instance ID.
type PairingClient struct {
	Purpose    PairingPurpose
	Name       string
	InstanceID string
}

// PairingSecretFunc makes what a client that completed a request receives.
type PairingSecretFunc func(ctx context.Context, client PairingClient) (PairingSecret, error)

var (
	instanceIDPattern = regexp.MustCompile(`^srv-[0-9a-f]{16}$`)
	mainNameUnwanted  = regexp.MustCompile(`[^a-z0-9]+`)
	maxMainNameLength = 40
)

// ValidInstanceID says whether id is a Legato instance ID: "srv-" and 16
// hexadecimal digits.
func ValidInstanceID(id string) bool { return instanceIDPattern.MatchString(id) }

// NormaliseMainName makes a main instance's name what a worker keeps:
// lowercase letters, digits and single dashes, at most 40. A name with
// nothing left, or too long, is refused.
func NormaliseMainName(name string) (string, error) {
	out := strings.Trim(mainNameUnwanted.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if out == "" {
		return "", fmt.Errorf("%w: %q has no letters or digits", ErrPairingClient, name)
	}
	if len(out) > maxMainNameLength {
		return "", fmt.Errorf("%w: %q is longer than %d characters", ErrPairingClient, name, maxMainNameLength)
	}
	return out, nil
}

// PairingService holds the one pairing request a server has at a time. It is
// in memory only: a restart ends the request.
type PairingService struct {
	mu       sync.Mutex
	secret   PairingSecretFunc
	mains    bool
	now      func() time.Time
	random   io.Reader
	onChange []func(PairingStatus)

	status   PairingStatus
	w        []byte
	sessions map[string]*pairing.Session
	shares   map[string][]byte
}

// NewPairingService makes the service; secret supplies what a paired client
// gets. It pairs phones only until AllowMains.
func NewPairingService(secret PairingSecretFunc) *PairingService {
	return &PairingService{
		secret: secret,
		now:    time.Now,
		random: rand.Reader,
		status: PairingStatus{State: PairingNone},
	}
}

// OnChange registers fn to be told of every change of the request.
func (s *PairingService) OnChange(fn func(PairingStatus)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onChange = append(s.onChange, fn)
}

// AllowMains lets requests pair main instances: the secret callback can
// make what a main gets.
func (s *PairingService) AllowMains() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mains = true
}

// MainsAllowed says whether this server pairs main instances.
func (s *PairingService) MainsAllowed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mains
}

// SetClock replaces the clock requests expire by.
func (s *PairingService) SetClock(now func() time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.now = now
}

// Open starts a new request for a phone, replacing any other.
func (s *PairingService) Open() (PairingStatus, error) { return s.OpenFor(PairingPhone) }

// OpenFor starts a new request for a phone or a main instance, replacing any
// other.
func (s *PairingService) OpenFor(purpose PairingPurpose) (PairingStatus, error) {
	if purpose == "" {
		purpose = PairingPhone
	}
	if purpose != PairingPhone && (purpose != PairingMain || !s.MainsAllowed()) {
		return PairingStatus{}, fmt.Errorf("%w: %q", ErrPairingPurpose, purpose)
	}
	phrase, err := pairing.NewPhrase(s.random)
	if err != nil {
		return PairingStatus{}, err
	}
	s.mu.Lock()
	s.status = PairingStatus{
		State:        PairingOpen,
		Purpose:      purpose,
		Phrase:       phrase,
		ExpiresAt:    s.now().Add(PairingLifetime),
		AttemptsLeft: PairingAttempts,
	}
	s.w = pairing.PhraseScalar(phrase)
	s.sessions = map[string]*pairing.Session{}
	s.shares = map[string][]byte{}
	st := s.status
	s.mu.Unlock()
	s.notify(st)
	return st, nil
}

// Status is the request as it stands now.
func (s *PairingService) Status() PairingStatus {
	s.mu.Lock()
	expired := s.expireLocked()
	st := s.status
	s.mu.Unlock()
	if expired {
		s.notify(st)
	}
	return st
}

// Cancel ends an open request.
func (s *PairingService) Cancel() {
	s.mu.Lock()
	s.expireLocked()
	if s.status.State != PairingOpen {
		s.mu.Unlock()
		return
	}
	s.closeLocked(PairingCancelled)
	st := s.status
	s.mu.Unlock()
	s.notify(st)
}

// Exchange is the first step for a phone: its SPAKE2 share in, a session and
// the server's share out. Each exchange is one of the request's attempts.
func (s *PairingService) Exchange(phoneShare []byte) (string, []byte, error) {
	s.mu.Lock()
	expired := s.expireLocked()
	if s.status.State != PairingOpen || s.status.AttemptsLeft <= 0 {
		st := s.status
		s.mu.Unlock()
		if expired {
			s.notify(st)
		}
		return "", nil, ErrPairingClosed
	}
	protocol := pairing.Protocol
	if s.status.Purpose == PairingMain {
		protocol = pairing.MainProtocol
	}
	session, share, err := pairing.Start(pairing.PartyB, protocol, s.w, s.random)
	if err != nil {
		s.mu.Unlock()
		return "", nil, err
	}
	id := make([]byte, 16)
	if _, err := io.ReadFull(s.random, id); err != nil {
		s.mu.Unlock()
		return "", nil, err
	}
	key := hex.EncodeToString(id)
	s.status.AttemptsLeft--
	s.sessions[key] = session
	s.shares[key] = phoneShare
	st := s.status
	s.mu.Unlock()
	s.notify(st)
	return key, share, nil
}

// Confirm is the second step: the client's confirmation and its sealed
// description in, the server's confirmation and the sealed secret out. A
// phone's description is optional; a main's must name it and its instance.
func (s *PairingService) Confirm(ctx context.Context, sessionID string, phoneConfirmation, sealedPhone []byte) ([]byte, []byte, error) {
	s.mu.Lock()
	expired := s.expireLocked()
	session, ok := s.sessions[sessionID]
	phoneShare := s.shares[sessionID]
	if s.status.State != PairingOpen || !ok {
		st := s.status
		s.mu.Unlock()
		if expired {
			s.notify(st)
		}
		return nil, nil, ErrPairingClosed
	}
	delete(s.sessions, sessionID)
	delete(s.shares, sessionID)

	keys, err := session.Finish(phoneShare)
	if err != nil {
		s.failLocked()
		st := s.status
		s.mu.Unlock()
		s.notify(st)
		return nil, nil, ErrPairingBadShare
	}
	if !keys.Confirms(phoneConfirmation) {
		s.failLocked()
		st := s.status
		s.mu.Unlock()
		s.notify(st)
		return nil, nil, ErrPairingMismatch
	}
	client := PairingClient{Purpose: s.status.Purpose}
	if client.Purpose == PairingMain {
		client, err = openMainDescription(keys.Key, sealedPhone)
		if err != nil {
			s.mu.Unlock()
			return nil, nil, err
		}
	} else {
		var phone struct {
			Device string `json:"device"`
		}
		if plain, err := pairing.Open(keys.Key, pairing.LabelPhone, sealedPhone); err == nil {
			_ = json.Unmarshal(plain, &phone)
		}
		client.Name = phone.Device
	}
	secret, err := s.secret(ctx, client)
	if err != nil {
		s.mu.Unlock()
		return nil, nil, err
	}
	payload, err := json.Marshal(secret)
	if err != nil {
		s.mu.Unlock()
		return nil, nil, err
	}
	sealed, err := pairing.Seal(keys.Key, pairing.LabelServer, payload, s.random)
	if err != nil {
		s.mu.Unlock()
		return nil, nil, err
	}
	s.closeLocked(PairingPaired)
	s.status.Device = client.Name
	st := s.status
	s.mu.Unlock()
	s.notify(st)
	return keys.Confirmation, sealed, nil
}

// openMainDescription reads what a main instance sealed: its name and its
// instance ID, both required.
func openMainDescription(key, sealed []byte) (PairingClient, error) {
	plain, err := pairing.Open(key, pairing.LabelMain, sealed)
	if err != nil {
		return PairingClient{}, fmt.Errorf("%w: its description does not open", ErrPairingClient)
	}
	var main struct {
		Name       string `json:"name"`
		InstanceID string `json:"instance_id"`
	}
	if err := json.Unmarshal(plain, &main); err != nil {
		return PairingClient{}, fmt.Errorf("%w: its description is not JSON", ErrPairingClient)
	}
	name, err := NormaliseMainName(main.Name)
	if err != nil {
		return PairingClient{}, err
	}
	if !ValidInstanceID(main.InstanceID) {
		return PairingClient{}, fmt.Errorf("%w: %q is not an instance ID", ErrPairingClient, main.InstanceID)
	}
	return PairingClient{Purpose: PairingMain, Name: name, InstanceID: main.InstanceID}, nil
}

func (s *PairingService) failLocked() {
	if s.status.AttemptsLeft <= 0 {
		s.closeLocked(PairingExhausted)
	}
}

func (s *PairingService) expireLocked() bool {
	if s.status.State == PairingOpen && s.now().After(s.status.ExpiresAt) {
		s.closeLocked(PairingExpired)
		return true
	}
	return false
}

func (s *PairingService) closeLocked(state PairingState) {
	s.status.State = state
	s.status.Phrase = nil
	s.w = nil
	s.sessions = nil
	s.shares = nil
}

func (s *PairingService) notify(st PairingStatus) {
	s.mu.Lock()
	fns := append([]func(PairingStatus){}, s.onChange...)
	s.mu.Unlock()
	for _, fn := range fns {
		fn(st)
	}
}

// PairingSecrets makes what each client that pairs with this server gets,
// as a PairingSecretFunc: a phone gets the full web token; a main instance
// gets a token of its own that can only hand off (CreateMain), and this
// server's instance ID. Both get this server's name and the certificate to
// trust.
type PairingSecrets struct {
	// WebToken is the token the web server checks.
	WebToken func() string
	// Name is this server's name, as a phone or a main lists it.
	Name string
	// CACertificate reads the PEM to trust for HTTPS, "" for none.
	CACertificate func() (string, error)
	// Mains mints main instances' tokens; nil pairs no main.
	Mains *IntegrationTokenService
	// InstanceID is this server's instance ID.
	InstanceID func(ctx context.Context) (string, error)
}

// Secret is the PairingSecretFunc.
func (p PairingSecrets) Secret(ctx context.Context, client PairingClient) (PairingSecret, error) {
	secret := PairingSecret{Name: p.Name}
	if p.CACertificate != nil {
		pem, err := p.CACertificate()
		if err != nil {
			return PairingSecret{}, err
		}
		secret.CACertificate = pem
	}
	if client.Purpose != PairingMain {
		token := ""
		if p.WebToken != nil {
			token = p.WebToken()
		}
		if token == "" {
			return PairingSecret{}, errors.New("this server has no token")
		}
		secret.Token = token
		return secret, nil
	}
	if p.Mains == nil || p.InstanceID == nil {
		return PairingSecret{}, fmt.Errorf("%w: %q", ErrPairingPurpose, client.Purpose)
	}
	id, err := p.InstanceID(ctx)
	if err != nil {
		return PairingSecret{}, err
	}
	token, err := p.Mains.CreateMain(ctx, client.Name, client.InstanceID)
	if err != nil {
		return PairingSecret{}, err
	}
	secret.Token, secret.InstanceID = token, id
	return secret, nil
}
