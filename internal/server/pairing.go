package server

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/cpave3/legato/internal/service"
)

// MsgPairingChanged says the pairing request changed; clients read it again.
const MsgPairingChanged = "pairing_changed"

// Pairing by phrase. The person opens a request (POST /api/pairing, with the
// token) and reads its phrase; the phone pairs through /api/pair/exchange and
// /api/pair/confirm, which take no token: the phrase is what they check.

// SetPairingService enables pairing by phrase.
func (s *Server) SetPairingService(svc *service.PairingService) {
	s.pairing = svc
	svc.OnChange(func(st service.PairingStatus) {
		s.hub.Broadcast(WSMessage{Type: MsgPairingChanged, Status: string(st.State)})
	})
}

// SetPairingAddresses gives the addresses (scheme://host:port) a phone on the
// local network can reach this server at, for the QR code of a request opened
// from a browser on this machine.
func (s *Server) SetPairingAddresses(addresses func() []string) {
	s.pairingAddresses = addresses
}

// pairingView is a request as a browser or legato pair is shown it: with
// pair_uri, the QR code that pairs a phone with this request's phrase.
type pairingView struct {
	service.PairingStatus
	PairURI string `json:"pair_uri,omitempty"`
}

func (s *Server) viewPairing(r *http.Request, st service.PairingStatus) pairingView {
	view := pairingView{PairingStatus: st}
	if st.State != service.PairingOpen || len(st.Phrase) == 0 {
		return view
	}
	address := ""
	if host := r.Host; host != "" && !isLoopbackHost(host) {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		address = scheme + "://" + host
	} else if s.pairingAddresses != nil {
		if found := s.pairingAddresses(); len(found) > 0 {
			address = found[0]
		}
	}
	if address == "" {
		return view
	}
	view.PairURI = "legato://pair?" + url.Values{"url": {address}, "phrase": {strings.Join(st.Phrase, "-")}}.Encode()
	return view
}

func isLoopbackHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

// isPairingStep reports whether r is one of the phone's two steps, which the
// auth middleware lets through without the token.
func isPairingStep(r *http.Request) bool {
	return r.Method == http.MethodPost && (r.URL.Path == "/api/pair/exchange" || r.URL.Path == "/api/pair/confirm")
}

func (s *Server) pairingHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.pairing == nil {
			writeJSONError(w, http.StatusNotFound, "this server does not pair by phrase")
			return
		}
		switch r.Method {
		case http.MethodGet:
			writeJSONValue(w, http.StatusOK, s.viewPairing(r, s.pairing.Status()))
		case http.MethodPost:
			st, err := s.pairing.Open()
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSONValue(w, http.StatusOK, s.viewPairing(r, st))
		case http.MethodDelete:
			s.pairing.Cancel()
			writeJSONValue(w, http.StatusOK, s.pairing.Status())
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

// readPairingBody decodes a step's JSON body into the named base64 fields.
func readPairingBody(w http.ResponseWriter, r *http.Request, into any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	if err := json.NewDecoder(r.Body).Decode(into); err != nil {
		writeJSONError(w, http.StatusBadRequest, "the pairing message is not valid")
		return false
	}
	return true
}

func decodeB64(fields ...string) ([][]byte, bool) {
	out := make([][]byte, len(fields))
	for i, f := range fields {
		b, err := base64.StdEncoding.DecodeString(f)
		if err != nil {
			return nil, false
		}
		out[i] = b
	}
	return out, true
}

func (s *Server) pairExchangeHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if s.pairing == nil {
			writeJSONError(w, http.StatusNotFound, "this server does not pair by phrase")
			return
		}
		var body struct {
			Share string `json:"share"`
		}
		if !readPairingBody(w, r, &body) {
			return
		}
		decoded, ok := decodeB64(body.Share)
		if !ok {
			writeJSONError(w, http.StatusBadRequest, "the pairing message is not valid")
			return
		}
		session, share, err := s.pairing.Exchange(decoded[0])
		if err != nil {
			writePairingError(w, err)
			return
		}
		writeJSONValue(w, http.StatusOK, map[string]string{
			"session": session,
			"share":   base64.StdEncoding.EncodeToString(share),
		})
	}
}

func (s *Server) pairConfirmHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if s.pairing == nil {
			writeJSONError(w, http.StatusNotFound, "this server does not pair by phrase")
			return
		}
		var body struct {
			Session      string `json:"session"`
			Confirmation string `json:"confirmation"`
			Phone        string `json:"phone"`
		}
		if !readPairingBody(w, r, &body) {
			return
		}
		decoded, ok := decodeB64(body.Confirmation, body.Phone)
		if !ok {
			writeJSONError(w, http.StatusBadRequest, "the pairing message is not valid")
			return
		}
		confirmation, sealed, err := s.pairing.Confirm(body.Session, decoded[0], decoded[1])
		if err != nil {
			writePairingError(w, err)
			return
		}
		writeJSONValue(w, http.StatusOK, map[string]string{
			"confirmation": base64.StdEncoding.EncodeToString(confirmation),
			"sealed":       base64.StdEncoding.EncodeToString(sealed),
		})
	}
}

func writePairingError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrPairingClosed):
		writeJSONError(w, http.StatusGone, "No pairing request is open. Start one with legato pair, or in Settings › Pair a device.")
	case errors.Is(err, service.ErrPairingMismatch):
		writeJSONError(w, http.StatusForbidden, "The phrase does not match.")
	case errors.Is(err, service.ErrPairingBadShare):
		writeJSONError(w, http.StatusBadRequest, "the pairing message is not valid")
	default:
		writeJSONError(w, http.StatusInternalServerError, err.Error())
	}
}

func writeJSONValue(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSONValue(w, status, map[string]string{"error": message})
}
