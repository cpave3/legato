package discovery

import (
	"net"
	"reflect"
	"testing"
)

func TestOnlyRealNetworkInterfacesAreAnnouncedOn(t *testing.T) {
	up := net.FlagUp | net.FlagMulticast | net.FlagBroadcast
	ifaces := []net.Interface{
		{Name: "lo", Flags: net.FlagUp | net.FlagLoopback | net.FlagMulticast},
		{Name: "enp5s0", Flags: up},
		{Name: "wlan0", Flags: up},
		{Name: "docker0", Flags: up},
		{Name: "br-e8dfcb19904f", Flags: up},
		{Name: "veth12ab", Flags: up},
		{Name: "virbr0", Flags: up},
		{Name: "podman0", Flags: up},
		{Name: "tailscale0", Flags: up},
		{Name: "eth1", Flags: net.FlagMulticast}, // down
		{Name: "tun0", Flags: net.FlagUp | net.FlagPointToPoint},
	}
	var names []string
	for _, i := range announceOn(ifaces) {
		names = append(names, i.Name)
	}
	if want := []string{"enp5s0", "wlan0"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("announced on %v, want %v", names, want)
	}
}
