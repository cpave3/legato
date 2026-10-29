package attest

import (
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

// GoogleRevocationURL is Google's list of revoked attestation certificates.
const GoogleRevocationURL = "https://android.googleapis.com/attestation/status"

// revocationLife is how long a read list is used before it is read again.
const revocationLife = 24 * time.Hour

// RevocationList reads Google's attestation status list and keeps it a
// day. A failed read keeps the last list; with no list ever read, it has
// no answer, and a chain is refused.
type RevocationList struct {
	URL    string
	Client *http.Client
	Now    func() time.Time

	mu      sync.Mutex
	entries map[string]bool
	readAt  time.Time
}

func (r *RevocationList) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// Revoked reports whether a certificate serial is revoked or suspended.
func (r *RevocationList) Revoked(serial *big.Int) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.entries == nil || r.now().Sub(r.readAt) > revocationLife {
		entries, err := r.read()
		switch {
		case err == nil:
			r.entries, r.readAt = entries, r.now()
		case r.entries == nil:
			return false, err
		}
	}
	return r.entries[strings.ToLower(serial.Text(16))], nil
}

func (r *RevocationList) read() (map[string]bool, error) {
	url := r.URL
	if url == "" {
		url = GoogleRevocationURL
	}
	client := r.Client
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the attestation status list: %s", resp.Status)
	}
	var body struct {
		Entries map[string]struct {
			Status string `json:"status"`
		} `json:"entries"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&body); err != nil {
		return nil, fmt.Errorf("the attestation status list does not read: %w", err)
	}
	entries := make(map[string]bool, len(body.Entries))
	for serial, e := range body.Entries {
		if e.Status == "REVOKED" || e.Status == "SUSPENDED" {
			entries[strings.ToLower(strings.TrimLeft(serial, "0"))] = true
		}
	}
	return entries, nil
}
