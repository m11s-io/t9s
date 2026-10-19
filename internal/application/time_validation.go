package application

import (
	"fmt"
	"net/netip"
	"strings"
	"unicode"
)

// ValidateTimeServer accepts a hostname or a bare IP address, never a URL,
// port, path, or whitespace/control-bearing value.
func ValidateTimeServer(input string) (string, error) {
	invalid := fmt.Errorf("enter a hostname or bare IP address")
	for _, r := range input {
		if unicode.IsControl(r) {
			return "", invalid
		}
	}
	server := strings.TrimSpace(input)
	if server == "" || strings.ContainsFunc(server, unicode.IsSpace) {
		return "", invalid
	}
	if addr, err := netip.ParseAddr(server); err == nil && addr.Zone() == "" {
		return server, nil
	}
	host := strings.TrimSuffix(server, ".")
	if len(host) == 0 || len(host) > 253 {
		return "", invalid
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", invalid
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
				return "", invalid
			}
		}
	}
	return server, nil
}
