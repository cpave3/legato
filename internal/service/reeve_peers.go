package service

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/engine/credential"
	"github.com/cpave3/legato/internal/engine/store"
)

// Reeve to Reeve (docs/ai/reeve.md § Reeve to Reeve): a Reeve messages the
// Reeves of instances paired with its own, by a main and worker pairing
// (the main's token has the reeve scope) or a Reeve pairing (purpose
// reeve). The Reeve prime sends, with its message, its address and a
// reeve token for itself, so the other side can answer; when that address
// cannot be reached, the answer waits for the prime's next message.

// ReeveMessagePath is where an instance's Reeve is messaged.
const ReeveMessagePath = "/api/integrations/reeve/messages"

var (
	// ErrReevePeerUnknown: no paired instance by that name or ID.
	ErrReevePeerUnknown = errors.New("no paired instance by that name: list them with instance_status, or pair one in Settings › Reeve")
	// ErrPeerRefused: this instance takes no messages from other Reeves
	// while none runs (reeve.peer: refuse).
	ErrPeerRefused = errors.New("this instance's Reeve takes no messages from other instances now")
)

// Peer roles, as a message names its sender.
const (
	PeerViaPairing = "pairing"
	PeerViaWorker  = "worker"
	PeerViaMain    = "main"
	PeerViaPrime   = "prime"
)

// ReeveSelf is this instance as another Reeve sees it.
type ReeveSelf struct {
	InstanceID    string
	Name          string
	Role          string
	Address       string
	CACertificate string
}

// PeerInfo is the instance a message came from.
type PeerInfo struct {
	InstanceID string `json:"instance_id"`
	Name       string `json:"name"`
	Role       string `json:"role"`
}

// PeerContact is how to answer the sender: its address, a reeve token for
// it, and the certificate to trust.
type PeerContact struct {
	Address       string `json:"address"`
	Token         string `json:"token"`
	CACertificate string `json:"ca_certificate,omitempty"`
}

// PeerMessage is a message from one Reeve to another.
type PeerMessage struct {
	From  PeerInfo     `json:"from"`
	Text  string       `json:"text"`
	Reply *PeerContact `json:"reply,omitempty"`
}

// PeerAnswer is what the receiving side answers: the replies that waited
// for this connection.
type PeerAnswer struct {
	Status  string   `json:"status"` // delivered, waiting (for the person)
	Replies []string `json:"replies,omitempty"`
}

// ReevePeerOptions wires a ReevePeerService.
type ReevePeerOptions struct {
	Store   *store.Store
	Tokens  *IntegrationTokenService
	Secrets WorkerSecrets
	Workers *WorkerService
	Self    func(ctx context.Context) ReeveSelf
	// Deliver gives a peer's message to this instance's Reeve, by
	// reeve.peer; ErrPeerAsk when it waits for the person, ErrPeerRefused.
	Deliver func(ctx context.Context, from PeerInfo, text string) error
	Timeout time.Duration
}

// ErrPeerAsk: the message waits for the person (reeve.peer: ask).
var ErrPeerAsk = errors.New("the message waits for the person to let the Reeve talk")

// ReevePeerService messages other instances' Reeves and takes their
// messages.
type ReevePeerService struct{ opts ReevePeerOptions }

func NewReevePeerService(opts ReevePeerOptions) *ReevePeerService {
	if opts.Timeout == 0 {
		opts.Timeout = 15 * time.Second
	}
	return &ReevePeerService{opts: opts}
}

// Peer is an instance this Reeve can message, as Settings lists it.
type Peer struct {
	InstanceID string `json:"instance_id"`
	Name       string `json:"name"`
	Via        string `json:"via"`
	Address    string `json:"address,omitempty"`
}

// List is every instance this Reeve can message: workers of this main,
// Reeve pairings, and primes that gave a reply contact.
func (s *ReevePeerService) List(ctx context.Context) ([]Peer, error) {
	var peers []Peer
	if s.opts.Workers != nil {
		if workers, err := s.opts.Workers.List(ctx); err == nil {
			for _, w := range workers {
				peers = append(peers, Peer{InstanceID: w.InstanceID, Name: w.Name, Via: PeerViaWorker, Address: w.Address})
			}
		}
	}
	rows, err := s.opts.Store.ListReevePeers(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if containsPeer(peers, r.InstanceID) {
			continue
		}
		peers = append(peers, Peer{InstanceID: r.InstanceID, Name: r.Name, Via: r.Via, Address: r.Address})
	}
	return peers, nil
}

func containsPeer(peers []Peer, id string) bool {
	for _, p := range peers {
		if p.InstanceID == id {
			return true
		}
	}
	return false
}

// Pair runs a Reeve pairing with the instance at address, whose person
// opened a request (purpose reeve) and read out its phrase: this side gets
// a reeve token for it. Its Reeve gets one for this side with the first
// message.
func (s *ReevePeerService) Pair(ctx context.Context, address, phrase string) (Peer, error) {
	address, err := normaliseWorkerAddress(address)
	if err != nil {
		return Peer{}, err
	}
	words, err := parsePhrase(phrase)
	if err != nil {
		return Peer{}, err
	}
	self := s.opts.Self(ctx)
	name, err := NormaliseMainName(self.Name)
	if err != nil {
		name = "legato"
	}
	unpinned := (&WorkerService{opts: WorkerServiceOptions{Timeout: s.opts.Timeout}}).httpClient(&tls.Config{InsecureSkipVerify: true}) //nolint:gosec // the phrase authenticates the exchange
	secret, err := (&WorkerService{}).exchange(ctx, unpinned, address, words, name, self.InstanceID)
	if err != nil {
		return Peer{}, err
	}
	if !strings.HasPrefix(secret.Token, IntegrationTokenPrefix) || !ValidInstanceID(secret.InstanceID) {
		return Peer{}, fmt.Errorf("%w: it did not send a Reeve token; update it", ErrWorkerRefused)
	}
	row := store.ReevePeer{InstanceID: secret.InstanceID, Name: secret.Name, Via: PeerViaPairing, Address: address,
		CACertificate: secret.CACertificate, TokenSecret: reevePeerSecret(secret.InstanceID)}
	if err := s.opts.Secrets.Add(ctx, row.TokenSecret, credential.TypeValue, []byte(secret.Token)); err != nil {
		return Peer{}, fmt.Errorf("keeping its token: %w", err)
	}
	if err := s.opts.Store.SaveReevePeer(ctx, row); err != nil {
		return Peer{}, err
	}
	return Peer{InstanceID: row.InstanceID, Name: row.Name, Via: row.Via, Address: row.Address}, nil
}

func reevePeerSecret(instanceID string) string { return "reeve-peer-" + instanceID }

// Remove forgets a Reeve pairing or a prime's contact: its token here is
// removed, and the token it was given is revoked.
func (s *ReevePeerService) Remove(ctx context.Context, instanceID string) error {
	row, err := s.opts.Store.GetReevePeer(ctx, instanceID)
	if err != nil {
		return err
	}
	if row.TokenSecret != "" && s.opts.Secrets != nil {
		_ = s.opts.Secrets.Remove(ctx, row.TokenSecret)
	}
	if err := s.opts.Store.RevokeReevePeerTokens(ctx, instanceID); err != nil {
		return err
	}
	return s.opts.Store.DeleteReevePeer(ctx, instanceID)
}

// SendOutcome says how a message went: delivered to the other Reeve, or
// queued for its next connection.
type SendOutcome struct {
	To      Peer     `json:"to"`
	State   string   `json:"state"`
	Replies []string `json:"replies,omitempty"`
}

// Send messages the Reeve of a paired instance, by its name or instance
// ID. The message carries this instance's address and a reeve token for
// it, the first time, so the other side can answer. Replies that waited for
// this connection are delivered to this instance's Reeve.
func (s *ReevePeerService) Send(ctx context.Context, to, text string) (SendOutcome, error) {
	peers, err := s.List(ctx)
	if err != nil {
		return SendOutcome{}, err
	}
	var peer *Peer
	for i := range peers {
		if strings.EqualFold(peers[i].Name, to) || peers[i].InstanceID == to {
			peer = &peers[i]
			break
		}
	}
	if peer == nil {
		return SendOutcome{}, ErrReevePeerUnknown
	}
	self := s.opts.Self(ctx)
	msg := PeerMessage{From: PeerInfo{InstanceID: self.InstanceID, Name: self.Name, Role: self.Role}, Text: text}
	reply, err := s.replyContact(ctx, *peer, self)
	if err != nil {
		return SendOutcome{}, err
	}
	msg.Reply = reply
	var answer PeerAnswer
	if peer.Via == PeerViaWorker {
		client, cerr := s.opts.Workers.Client(ctx, peer.InstanceID)
		if cerr != nil {
			return SendOutcome{}, cerr
		}
		err = client.Call(ctx, http.MethodPost, ReeveMessagePath, msg, &answer)
	} else {
		err = s.callPeer(ctx, peer.InstanceID, msg, &answer)
	}
	if errors.Is(err, ErrWorkerUnreachable) && peer.Via == PeerViaPrime {
		// A prime behind NAT: the answer waits for its next message.
		if qerr := s.opts.Store.QueueReeveReply(ctx, peer.InstanceID, text); qerr != nil {
			return SendOutcome{}, qerr
		}
		return SendOutcome{To: *peer, State: "queued"}, nil
	}
	if err != nil {
		return SendOutcome{}, err
	}
	if reply != nil && reply.Token != "" {
		if row, gerr := s.opts.Store.GetReevePeer(ctx, peer.InstanceID); gerr == nil {
			row.ReplyGiven = true
			_ = s.opts.Store.SaveReevePeer(ctx, *row)
		} else {
			_ = s.opts.Store.SaveReevePeer(ctx, store.ReevePeer{InstanceID: peer.InstanceID, Name: peer.Name, Via: peer.Via, Address: peer.Address, ReplyGiven: true})
		}
	}
	for _, r := range answer.Replies {
		_ = s.deliver(ctx, PeerInfo{InstanceID: peer.InstanceID, Name: peer.Name, Role: peer.Via}, r)
	}
	return SendOutcome{To: *peer, State: firstNonEmpty(answer.Status, "delivered"), Replies: answer.Replies}, nil
}

// replyContact is this instance's address and, the first time for a peer,
// a reeve token for it: how its Reeve answers.
func (s *ReevePeerService) replyContact(ctx context.Context, peer Peer, self ReeveSelf) (*PeerContact, error) {
	contact := &PeerContact{Address: self.Address, CACertificate: self.CACertificate}
	if row, err := s.opts.Store.GetReevePeer(ctx, peer.InstanceID); err == nil && row.ReplyGiven {
		return contact, nil
	}
	token, err := s.opts.Tokens.CreateReevePeer(ctx, peer.Name, peer.InstanceID)
	if err != nil {
		return nil, err
	}
	contact.Token = token
	return contact, nil
}

func (s *ReevePeerService) callPeer(ctx context.Context, instanceID string, msg PeerMessage, into any) error {
	return s.callPeerAt(ctx, instanceID, http.MethodPost, ReeveMessagePath, msg, into)
}

func (s *ReevePeerService) callPeerAt(ctx context.Context, instanceID, method, path string, body, into any) error {
	row, err := s.opts.Store.GetReevePeer(ctx, instanceID)
	if err != nil {
		return ErrReevePeerUnknown
	}
	if row.Address == "" || row.TokenSecret == "" {
		return fmt.Errorf("%w: no address for %s", ErrWorkerUnreachable, row.Name)
	}
	secret, err := s.opts.Secrets.Get(ctx, credential.Ref{Scheme: "legato", Path: row.TokenSecret})
	if err != nil {
		return fmt.Errorf("reading the token for %s: %w", row.Name, err)
	}
	cfg := &tls.Config{}
	if row.CACertificate != "" {
		pool := x509.NewCertPool()
		if pool.AppendCertsFromPEM([]byte(row.CACertificate)) {
			cfg.RootCAs = pool
		}
	}
	client := (&WorkerService{opts: WorkerServiceOptions{Timeout: s.opts.Timeout}}).httpClient(cfg)
	return workerCall(ctx, client, method, strings.TrimRight(row.Address, "/")+path, string(secret.Value), body, into)
}

// Receive takes a message from another instance's Reeve: the sender is the
// token's instance (a peer never says who it is itself). Its reply contact
// is kept, the message is given to this Reeve, and the replies that waited
// for this connection are answered.
func (s *ReevePeerService) Receive(ctx context.Context, identity IntegrationIdentity, msg PeerMessage) (PeerAnswer, error) {
	from := PeerInfo{InstanceID: identity.InstanceID, Name: firstNonEmpty(msg.From.Name, identity.Name), Role: msg.From.Role}
	if !ValidInstanceID(from.InstanceID) {
		return PeerAnswer{}, errors.New("the sender has no instance ID")
	}
	if msg.Reply != nil && (msg.Reply.Address != "" || msg.Reply.Token != "") {
		row, err := s.opts.Store.GetReevePeer(ctx, from.InstanceID)
		if err != nil {
			via := PeerViaPrime
			if identity.Kind == IntegrationKindMain {
				via = PeerViaMain
			}
			row = &store.ReevePeer{InstanceID: from.InstanceID, Name: from.Name, Via: via}
		}
		// The sender called with a token this instance gave it: it can
		// already answer, and needs no other.
		row.ReplyGiven = true
		if msg.Reply.Address != "" {
			row.Address, row.CACertificate = msg.Reply.Address, msg.Reply.CACertificate
		}
		if msg.Reply.Token != "" && s.opts.Secrets != nil {
			row.TokenSecret = reevePeerSecret(from.InstanceID)
			if err := s.opts.Secrets.Add(ctx, row.TokenSecret, credential.TypeValue, []byte(msg.Reply.Token)); err != nil {
				return PeerAnswer{}, err
			}
		}
		if err := s.opts.Store.SaveReevePeer(ctx, *row); err != nil {
			return PeerAnswer{}, err
		}
	}
	answer := PeerAnswer{Status: "delivered"}
	switch err := s.deliver(ctx, from, msg.Text); {
	case errors.Is(err, ErrPeerAsk):
		answer.Status = "waiting"
	case err != nil:
		return PeerAnswer{}, err
	}
	replies, err := s.opts.Store.TakeReeveReplies(ctx, from.InstanceID)
	if err != nil {
		return PeerAnswer{}, err
	}
	answer.Replies = replies
	return answer, nil
}

func (s *ReevePeerService) deliver(ctx context.Context, from PeerInfo, text string) error {
	if s.opts.Deliver == nil {
		return ErrPeerRefused
	}
	return s.opts.Deliver(ctx, from, text)
}

// PeerFrame is a peer's message as the Reeve reads it.
func PeerFrame(from PeerInfo, text string) string {
	role := from.Role
	if role == "" {
		role = "peer"
	}
	return fmt.Sprintf("[from the Reeve on %s · %s] %s", from.Name, role, text)
}

// ReeveApprovalsPath is where an instance lists its pending Reeve change
// sets for a paired instance's tray (scope reeve).
const ReeveApprovalsPath = "/api/integrations/reeve/approvals"

// PeerApproval is a pending Reeve change set, as a paired instance's tray
// shows it.
type PeerApproval struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Summary   string `json:"summary,omitempty"`
	Signed    bool   `json:"signed,omitempty"`
	Verified  bool   `json:"verified,omitempty"`
	CreatedAt string `json:"created_at"`
}

// OwnApprovals lists this instance's Reeve change sets waiting for the
// person: what a paired instance's tray shows of it.
func (s *ReevePeerService) OwnApprovals(ctx context.Context) ([]PeerApproval, error) {
	rows, err := s.opts.Store.ListApprovals(ctx, store.ApprovalFilter{Status: store.ApprovalPending, Source: ApprovalSourceReeveChangeSet})
	if err != nil {
		return nil, err
	}
	out := make([]PeerApproval, 0, len(rows))
	for _, a := range rows {
		out = append(out, PeerApproval{ID: a.ID, Title: a.Title, Summary: a.Summary, Signed: a.Signed, Verified: a.Verified, CreatedAt: a.CreatedAt})
	}
	return out, nil
}

// InstanceApproval is another instance's pending change set, marked with
// its instance, with the links to decide it there.
type InstanceApproval struct {
	PeerApproval
	InstanceID string `json:"instance_id"`
	Instance   string `json:"instance"`
	// URL opens that instance's page on the approval; InstanceURL its page.
	URL         string `json:"url"`
	InstanceURL string `json:"instance_url"`
}

// InstanceApprovals is every paired instance's pending change sets, and
// the instances that did not answer.
type InstanceApprovals struct {
	Approvals   []InstanceApproval `json:"approvals"`
	Unreachable []string           `json:"unreachable"`
}

// PeerApprovals asks each paired instance for its pending Reeve change
// sets. They are decided there, with a device enrolled there.
func (s *ReevePeerService) PeerApprovals(ctx context.Context) (InstanceApprovals, error) {
	out := InstanceApprovals{Approvals: []InstanceApproval{}, Unreachable: []string{}}
	peers, err := s.List(ctx)
	if err != nil {
		return out, err
	}
	type answer struct {
		peer Peer
		list []PeerApproval
		err  error
	}
	answers := make([]answer, len(peers))
	var wg sync.WaitGroup
	for i, p := range peers {
		if p.Address == "" {
			continue
		}
		wg.Add(1)
		go func(i int, p Peer) {
			defer wg.Done()
			var body struct {
				Approvals []PeerApproval `json:"approvals"`
			}
			var err error
			if p.Via == PeerViaWorker {
				client, cerr := s.opts.Workers.Client(ctx, p.InstanceID)
				if cerr != nil {
					err = cerr
				} else {
					err = client.Call(ctx, http.MethodGet, ReeveApprovalsPath, nil, &body)
				}
			} else {
				err = s.callPeerAt(ctx, p.InstanceID, http.MethodGet, ReeveApprovalsPath, nil, &body)
			}
			answers[i] = answer{peer: p, list: body.Approvals, err: err}
		}(i, p)
	}
	wg.Wait()
	for _, a := range answers {
		if a.peer.InstanceID == "" {
			continue
		}
		if a.err != nil {
			out.Unreachable = append(out.Unreachable, a.peer.Name)
			continue
		}
		base := strings.TrimRight(a.peer.Address, "/")
		for _, item := range a.list {
			out.Approvals = append(out.Approvals, InstanceApproval{PeerApproval: item, InstanceID: a.peer.InstanceID, Instance: a.peer.Name,
				URL: base + "/?approval=" + url.QueryEscape(item.ID), InstanceURL: base + "/"})
		}
	}
	return out, nil
}
