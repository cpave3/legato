package main

import (
	"testing"

	"github.com/cpave3/legato/config"
)

// The link in an approval notification names the address a phone uses for
// this server: web.public_url, else the TLS host name with the web port,
// else a local network address.
func TestTheApprovalLinkNamesTheAddressAPhoneUses(t *testing.T) {
	ips := func() []string { return []string{"192.168.1.20"} }
	cases := []struct {
		name string
		cfg  config.Config
		want string
	}{
		{"public url", config.Config{Web: config.WebConfig{PublicURL: "https://legato.example.net", Port: "3080"}}, "https://legato.example.net"},
		{"tls host name", config.Config{Web: config.WebConfig{Port: "3080", TLS: config.TLSConfig{Enabled: true, Hostname: "erebus.pavey.network"}}}, "https://erebus.pavey.network:3080"},
		{"local address", config.Config{Web: config.WebConfig{Port: "3080"}}, "http://192.168.1.20:3080"},
	}
	for _, c := range cases {
		if got := approvalServerURL(&c.cfg, "", ips); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
	tls := config.Config{Web: config.WebConfig{Port: "3080", TLS: config.TLSConfig{Enabled: true}}}
	if got := approvalServerURL(&tls, "3443", ips); got != "https://192.168.1.20:3443" {
		t.Errorf("tls without a host name, listening on 3443: %q", got)
	}
	if got := approvalServerURL(&config.Config{}, "", func() []string { return nil }); got != "" {
		t.Errorf("with no address known: %q", got)
	}
}
