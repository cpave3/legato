package server

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// SetDeviceService serves the devices that sign approval decisions.
func (s *Server) SetDeviceService(devices *service.DeviceService) {
	s.devices = devices
}

func (s *Server) requireDeviceService(w http.ResponseWriter) bool {
	if s.devices != nil {
		return true
	}
	s.writeError(w, http.StatusServiceUnavailable, "signed approvals are not available on this server")
	return false
}

// devicesHandler serves GET /api/devices and POST /api/devices (enroll).
func (s *Server) devicesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.requireDeviceService(w) {
			return
		}
		ctx := r.Context()
		switch r.Method {
		case http.MethodGet:
			devices, err := s.devices.Devices(ctx)
			if err != nil {
				s.writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			enrolled, _ := s.devices.Enrolled(ctx)
			serverID, _ := s.devices.ServerID(ctx)
			s.writeJSON(w, http.StatusOK, map[string]any{"devices": devices, "enrolled": enrolled, "server_id": serverID})
		case http.MethodPost:
			var in struct {
				Name       string          `json:"name"`
				Kind       string          `json:"kind"`
				Chain      []string        `json:"chain"`
				Challenge  string          `json:"challenge"`
				Credential json.RawMessage `json:"credential"`
				Origin     string          `json:"origin"`
			}
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in); err != nil {
				s.writeError(w, http.StatusBadRequest, "invalid enrollment")
				return
			}
			if in.Kind == "passkey" {
				device, err := s.devices.Enroll(ctx, service.EnrollRequest{Name: in.Name, Kind: in.Kind, Credential: in.Credential, Origin: pageOrigin(r, in.Origin)})
				if err != nil {
					s.writeError(w, http.StatusForbidden, err.Error())
					return
				}
				s.writeJSON(w, http.StatusCreated, device)
				return
			}
			challenge, err := base64.RawURLEncoding.DecodeString(in.Challenge)
			if err != nil {
				s.writeError(w, http.StatusBadRequest, "the challenge does not read")
				return
			}
			chain := make([][]byte, 0, len(in.Chain))
			for _, c := range in.Chain {
				der, err := base64.StdEncoding.DecodeString(c)
				if err != nil {
					s.writeError(w, http.StatusBadRequest, "a certificate of the chain does not read")
					return
				}
				chain = append(chain, der)
			}
			device, err := s.devices.Enroll(ctx, service.EnrollRequest{Name: in.Name, Kind: in.Kind, Chain: chain, Challenge: challenge})
			if err != nil {
				s.writeError(w, http.StatusForbidden, err.Error())
				return
			}
			s.writeJSON(w, http.StatusCreated, device)
		default:
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	}
}

// deviceChallengeHandler serves POST /api/devices/challenge: a challenge a
// phone puts in the key it makes to enroll.
func (s *Server) deviceChallengeHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.requireDeviceService(w) {
			return
		}
		challenge, expires, err := s.devices.EnrollChallenge(r.Context())
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		serverID, _ := s.devices.ServerID(r.Context())
		s.writeJSON(w, http.StatusOK, map[string]string{"challenge": base64.RawURLEncoding.EncodeToString(challenge), "expires_at": expires.UTC().Format(time.RFC3339), "server_id": serverID})
	}
}

// pageOrigin is the origin of the page that sent a request: the browser's
// Origin header, else what the page says.
func pageOrigin(r *http.Request, said string) string {
	if origin := r.Header.Get("Origin"); origin != "" && origin != "null" {
		return origin
	}
	return said
}

// passkeyOptionsHandler serves POST /api/devices/passkey/options: the
// options for navigator.credentials.create on the page that asks, whose
// host name the passkey will belong to.
func (s *Server) passkeyOptionsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.requireDeviceService(w) {
			return
		}
		var in struct {
			Origin string `json:"origin"`
		}
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in)
		options, err := s.devices.PasskeyOptions(r.Context(), pageOrigin(r, in.Origin))
		if err != nil {
			s.writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(options)
	}
}

// deviceHandler serves DELETE /api/devices/{id}: a signed approval to
// remove an enrolled device, or the removal of one still waiting.
func (s *Server) deviceHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.requireDeviceService(w) {
			return
		}
		approvalID, err := s.devices.Remove(r.Context(), r.PathValue("id"))
		switch {
		case errors.Is(err, store.ErrNotFound):
			s.writeError(w, http.StatusNotFound, "no such device")
		case err != nil:
			s.writeError(w, http.StatusInternalServerError, err.Error())
		case approvalID == "":
			s.writeJSON(w, http.StatusOK, map[string]bool{"removed": true})
		default:
			s.writeJSON(w, http.StatusAccepted, map[string]string{"approval_id": approvalID})
		}
	}
}

// approvalChallengeHandler serves POST /api/approvals/{id}/challenge: the
// canonical request and a one-time challenge for an enrolled device.
func (s *Server) approvalChallengeHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !s.requireDeviceService(w) {
			return
		}
		var in struct {
			Device string `json:"device"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in); err != nil {
			s.writeError(w, http.StatusBadRequest, "invalid challenge request")
			return
		}
		c, err := s.devices.DecisionChallenge(r.Context(), r.PathValue("id"), in.Device)
		if err != nil {
			s.writeApprovalError(w, err)
			return
		}
		out := map[string]any{
			"canonical": base64.StdEncoding.EncodeToString(c.Canonical), "challenge": c.Challenge, "expires_at": c.ExpiresAt.UTC().Format(time.RFC3339),
		}
		if c.Passkey != nil {
			out["passkey"] = c.Passkey
		}
		s.writeJSON(w, http.StatusOK, out)
	}
}
