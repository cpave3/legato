package service

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/engine/pairing"
)

// A pairing request lets one phone pair by a four-word phrase. It is open for
// PairingLifetime, allows PairingAttempts tries, and ends when a phone pairs.
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

var (
	// ErrPairingClosed: no request is open (none, used, expired, out of tries, cancelled).
	ErrPairingClosed = errors.New("no pairing request is open")
	// ErrPairingMismatch: the phone's phrase is not the request's.
	ErrPairingMismatch = errors.New("the phrase does not match")
	// ErrPairingBadShare: what the phone sent is not part of an exchange.
	ErrPairingBadShare = errors.New("the pairing message is not valid")
)

// PairingStatus is what the person who opened a request is shown.
type PairingStatus struct {
	State        PairingState `json:"state"`
	Phrase       []string     `json:"phrase,omitempty"`
	ExpiresAt    time.Time    `json:"expires_at,omitzero"`
	AttemptsLeft int          `json:"attempts_left"`
	Device       string       `json:"device,omitempty"`
}

// PairingSecret is what a paired phone receives, sealed.
type PairingSecret struct {
	Token         string `json:"token"`
	Name          string `json:"name"`
	CACertificate string `json:"ca_certificate,omitempty"`
}

// PairingService holds the one pairing request a server has at a time. It is
// in memory only: a restart ends the request.
type PairingService struct {
	mu       sync.Mutex
	secret   func() (PairingSecret, error)
	now      func() time.Time
	random   io.Reader
	onChange []func(PairingStatus)

	status   PairingStatus
	w        []byte
	sessions map[string]*pairing.Session
	shares   map[string][]byte
}

// NewPairingService makes the service; secret supplies what a paired phone gets.
func NewPairingService(secret func() (PairingSecret, error)) *PairingService {
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

// Open starts a new request, replacing any other.
func (s *PairingService) Open() (PairingStatus, error) {
	phrase, err := pairing.NewPhrase(s.random)
	if err != nil {
		return PairingStatus{}, err
	}
	s.mu.Lock()
	s.status = PairingStatus{
		State:        PairingOpen,
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
	session, share, err := pairing.Start(pairing.PartyB, pairing.Protocol, s.w, s.random)
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

// Confirm is the second step: the phone's confirmation and its sealed
// description in, the server's confirmation and the sealed secret out.
func (s *PairingService) Confirm(sessionID string, phoneConfirmation, sealedPhone []byte) ([]byte, []byte, error) {
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
	var phone struct {
		Device string `json:"device"`
	}
	if plain, err := pairing.Open(keys.Key, pairing.LabelPhone, sealedPhone); err == nil {
		_ = json.Unmarshal(plain, &phone)
	}
	secret, err := s.secret()
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
	s.status.Device = phone.Device
	st := s.status
	s.mu.Unlock()
	s.notify(st)
	return keys.Confirmation, sealed, nil
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
