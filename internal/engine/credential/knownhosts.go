package credential

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"net"
	"os"
	"strings"

	"golang.org/x/crypto/ssh"
)

// KnownHosts names host keys from an OpenSSH known_hosts file. It is the
// host key, which the server proved, that names the host of a login, never
// what the agent's ssh says it connected to.
type KnownHosts struct {
	Path string
}

// Names returns the host names a key is listed under, as host or host:port.
// A hashed entry is named by the first candidate whose hash it holds.
// Revoked and certificate authority entries are not names.
func (k KnownHosts) Names(key ssh.PublicKey, candidates []string) []string {
	if k.Path == "" {
		return nil
	}
	data, err := os.ReadFile(k.Path)
	if err != nil {
		return nil
	}
	want := key.Marshal()
	var names []string
	seen := map[string]bool{}
	for len(data) > 0 {
		marker, hosts, pub, _, rest, err := ssh.ParseKnownHosts(data)
		if err != nil {
			break
		}
		data = rest
		if marker != "" || !bytes.Equal(pub.Marshal(), want) {
			continue
		}
		for _, h := range hosts {
			var name string
			if strings.HasPrefix(h, "|1|") {
				name = matchHashed(h, candidates)
			} else if !strings.ContainsAny(h, "*?!") {
				name = plainName(h)
			}
			if name != "" && !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	return names
}

// plainName turns [host]:port into host:port, and drops port 22.
func plainName(h string) string {
	if strings.HasPrefix(h, "[") {
		host, port, err := net.SplitHostPort(h)
		if err != nil {
			return ""
		}
		host = strings.Trim(host, "[]")
		if port == "22" {
			return host
		}
		return host + ":" + port
	}
	return h
}

func matchHashed(entry string, candidates []string) string {
	parts := strings.Split(entry, "|")
	if len(parts) != 4 {
		return ""
	}
	salt, err1 := base64.StdEncoding.DecodeString(parts[2])
	sum, err2 := base64.StdEncoding.DecodeString(parts[3])
	if err1 != nil || err2 != nil {
		return ""
	}
	for _, c := range candidates {
		form := c
		if host, port, err := net.SplitHostPort(c); err == nil && port != "22" {
			form = "[" + host + "]:" + port
		}
		if hmac.Equal(hostHMAC(salt, form), sum) {
			return c
		}
	}
	return ""
}

func hostHMAC(salt []byte, host string) []byte {
	mac := hmac.New(sha1.New, salt)
	mac.Write([]byte(host))
	return mac.Sum(nil)
}
