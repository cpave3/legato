package config

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// IntegrationsConfig holds services outside Legato that hand work to its
// agents. See docs/ai/integrations.md.
type IntegrationsConfig struct {
	// Argus delegates cases to agents and receives their replies. Unset,
	// its delegation routes answer 503 and agents get no reply_case tool.
	Argus *ArgusConfig `yaml:"argus"`
}

// ArgusConfig is the Argus integration.
type ArgusConfig struct {
	// URL is Argus's address: where replies go and where `legato integration
	// connect argus` registers. https, or http to a loopback address.
	URL string `yaml:"url"`
	// Delivery is auto (deliver at once) or ask (wait for the person's
	// approval). Default auto.
	Delivery string `yaml:"delivery"`
	// Agent is the tool an agent started for a delegation runs. Default
	// claude-code.
	Agent string `yaml:"agent"`
}

func normalizeIntegrations(cfg *Config) error {
	argus := cfg.Integrations.Argus
	if argus == nil {
		return nil
	}
	argus.URL = strings.TrimRight(strings.TrimSpace(argus.URL), "/")
	if argus.URL == "" {
		return fmt.Errorf("integrations.argus.url is required")
	}
	u, err := url.Parse(argus.URL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && !(u.Scheme == "http" && isLoopbackHost(u.Hostname()))) {
		return fmt.Errorf("integrations.argus.url %q: want an https address, or http to a loopback address", argus.URL)
	}
	switch argus.Delivery = strings.TrimSpace(argus.Delivery); argus.Delivery {
	case "":
		argus.Delivery = "auto"
	case "auto", "ask":
	default:
		return fmt.Errorf("integrations.argus.delivery %q: want auto or ask", argus.Delivery)
	}
	argus.Agent = strings.TrimSpace(argus.Agent)
	if argus.Agent == "" {
		argus.Agent = "claude-code"
	}
	if _, ok := supportedSwarmAgents[argus.Agent]; !ok {
		return fmt.Errorf("integrations.argus.agent %q: want claude-code, codex or chimera", argus.Agent)
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
