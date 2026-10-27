// Package discovery announces a Legato web server on the local network over
// mDNS (DNS-SD), so the Legato app can list it without an address typed in.
package discovery

import (
	"errors"
	"net"
	"strings"

	"github.com/libp2p/zeroconf/v2"
)

// ServiceType is the DNS-SD service a Legato web server announces.
const ServiceType = "_legato._tcp"

// Service is what is announced: the instance name the app shows, the port,
// and in a TXT record the scheme.
type Service struct {
	Name   string
	Port   int
	Scheme string
}

// Announce starts answering for s until stop is called.
func Announce(s Service) (stop func(), err error) {
	txt := []string{
		"scheme=" + s.Scheme,
		"phrase_pairing=1",
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	// zeroconf takes no interfaces to mean all of them.
	reachable := announceOn(ifaces)
	if len(reachable) == 0 {
		return nil, errors.New("no network interface a phone could reach this machine through")
	}
	server, err := zeroconf.Register(s.Name, ServiceType, "local.", s.Port, txt, reachable)
	if err != nil {
		return nil, err
	}
	return server.Shutdown, nil
}

// Interfaces of containers, virtual machines and VPNs, whose addresses a
// phone on the local network cannot reach.
var virtualPrefixes = []string{"docker", "br-", "veth", "virbr", "podman", "cni", "flannel", "tailscale", "zt", "wg", "tun", "tap"}

// announceOn picks the interfaces a phone on the same network can reach this
// machine through. The app is often given one address of an announcement,
// so an unreachable one must not be among them.
func announceOn(ifaces []net.Interface) []net.Interface {
	var out []net.Interface
	for _, i := range ifaces {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagMulticast == 0 || i.Flags&(net.FlagLoopback|net.FlagPointToPoint) != 0 {
			continue
		}
		virtual := false
		for _, prefix := range virtualPrefixes {
			if strings.HasPrefix(i.Name, prefix) {
				virtual = true
				break
			}
		}
		if !virtual {
			out = append(out, i)
		}
	}
	return out
}

// ReachableIPv4 lists this machine's IPv4 addresses on the interfaces it
// announces on, which a phone on the same network can use.
func ReachableIPv4() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []string
	for _, iface := range announceOn(ifaces) {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.To4() != nil && !ipnet.IP.IsLinkLocalUnicast() {
				out = append(out, ipnet.IP.String())
			}
		}
	}
	return out
}
