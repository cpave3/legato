// Package pairing pairs a phone with Legato by a short phrase: SPAKE2 (RFC 9382,
// SPAKE2-P256-SHA256-HKDF-HMAC) lets both sides prove they know the phrase
// without sending it, and gives them a key to send the token under.
package pairing

import (
	"bytes"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/big"

	"filippo.io/nistec"
)

// Party is the side of the exchange: A goes first and uses M, B uses N.
type Party int

const (
	PartyA Party = iota
	PartyB
)

// Config is what both parties agree on before the exchange.
type Config struct {
	IdentityA []byte
	IdentityB []byte
	AAD       []byte
}

// The P-256 points M and N of RFC 9382 § 6.
var (
	pointM = mustPoint("02886e2f97ace46e55ba9dd7242579f2993b64e16ef3dcab95afd497333d8fa12f")
	pointN = mustPoint("03d8bbd6c639c62937b04d997f38c3770719c629d7014d49a24b4f98baa1292b49")
	order  = mustInt("ffffffff00000000ffffffffffffffffbce6faada7179e84f3b9cac2fc632551")
)

func mustPoint(s string) *nistec.P256Point {
	b, _ := hex.DecodeString(s)
	p, err := nistec.NewP256Point().SetBytes(b)
	if err != nil {
		panic(err)
	}
	return p
}

func mustInt(s string) *big.Int {
	n, ok := new(big.Int).SetString(s, 16)
	if !ok {
		panic("bad integer")
	}
	return n
}

// scalarBytes is n as the 32-byte big-endian value nistec takes.
func scalarBytes(n *big.Int) []byte {
	out := make([]byte, 32)
	return n.FillBytes(out)
}

// Session is one party's half of an exchange, between its share and the peer's.
type Session struct {
	party  Party
	cfg    Config
	w      *big.Int
	secret *big.Int
	share  []byte
}

// Keys is what a finished exchange gives one party.
type Keys struct {
	// Key is Ke, the shared secret.
	Key []byte
	// Confirmation is this party's confirmation message, to send to the peer.
	Confirmation     []byte
	peerConfirmation []byte
}

// Confirms reports whether conf is the confirmation the peer must send.
func (k *Keys) Confirms(conf []byte) bool {
	return hmac.Equal(conf, k.peerConfirmation)
}

// Start begins an exchange with the password scalar w (32 bytes, big-endian).
// It reads the party's secret scalar from random, by rejection sampling, and
// returns the share to send to the peer.
func Start(party Party, cfg Config, w []byte, random io.Reader) (*Session, []byte, error) {
	if len(w) != 32 {
		return nil, nil, errors.New("pairing: the password scalar must be 32 bytes")
	}
	wInt := new(big.Int).Mod(new(big.Int).SetBytes(w), order)
	secret, err := randomScalar(random)
	if err != nil {
		return nil, nil, err
	}
	blind := pointM
	if party == PartyB {
		blind = pointN
	}
	x, err := nistec.NewP256Point().ScalarBaseMult(scalarBytes(secret))
	if err != nil {
		return nil, nil, err
	}
	wb, err := nistec.NewP256Point().ScalarMult(blind, scalarBytes(wInt))
	if err != nil {
		return nil, nil, err
	}
	share := nistec.NewP256Point().Add(x, wb).Bytes()
	return &Session{party: party, cfg: cfg, w: wInt, secret: secret, share: share}, share, nil
}

func randomScalar(random io.Reader) (*big.Int, error) {
	buf := make([]byte, 32)
	for range 64 {
		if _, err := io.ReadFull(random, buf); err != nil {
			return nil, fmt.Errorf("pairing: reading a random scalar: %w", err)
		}
		n := new(big.Int).SetBytes(buf)
		if n.Sign() > 0 && n.Cmp(order) < 0 {
			return n, nil
		}
	}
	return nil, errors.New("pairing: no usable random scalar")
}

// Finish takes the peer's share and derives the keys.
func (s *Session) Finish(peerShare []byte) (*Keys, error) {
	peer, err := nistec.NewP256Point().SetBytes(peerShare)
	if err != nil || len(peerShare) != 65 {
		return nil, errors.New("pairing: the peer's share is not a point")
	}
	peerBlind := pointN
	if s.party == PartyB {
		peerBlind = pointM
	}
	negW := new(big.Int).Sub(order, s.w)
	negW.Mod(negW, order)
	unblind, err := nistec.NewP256Point().ScalarMult(peerBlind, scalarBytes(negW))
	if err != nil {
		return nil, err
	}
	base := nistec.NewP256Point().Add(peer, unblind)
	k, err := nistec.NewP256Point().ScalarMult(base, scalarBytes(s.secret))
	if err != nil {
		return nil, err
	}
	kBytes := k.Bytes()
	if len(kBytes) != 65 {
		return nil, errors.New("pairing: the exchange gave no key")
	}

	pA, pB := s.share, peerShare
	if s.party == PartyB {
		pA, pB = peerShare, s.share
	}
	var tt bytes.Buffer
	for _, part := range [][]byte{s.cfg.IdentityA, s.cfg.IdentityB, pA, pB, kBytes, scalarBytes(s.w)} {
		_ = binary.Write(&tt, binary.LittleEndian, uint64(len(part)))
		tt.Write(part)
	}
	digest := sha256.Sum256(tt.Bytes())
	ke, ka := digest[:16], digest[16:]
	info := append([]byte("ConfirmationKeys"), s.cfg.AAD...)
	kc, err := hkdf.Key(sha256.New, ka, nil, string(info), 32)
	if err != nil {
		return nil, err
	}
	confA := mac(kc[:16], tt.Bytes())
	confB := mac(kc[16:], tt.Bytes())
	keys := &Keys{Key: append([]byte(nil), ke...), Confirmation: confA, peerConfirmation: confB}
	if s.party == PartyB {
		keys.Confirmation, keys.peerConfirmation = confB, confA
	}
	return keys, nil
}

func mac(key, msg []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(msg)
	return h.Sum(nil)
}
