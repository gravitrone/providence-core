package hooks

import (
	"fmt"
	"net"
	"net/netip"
	"strings"
)

type ssrfRule struct {
	prefix netip.Prefix
	label  string
}

var (
	lookupIP = net.LookupIP

	blockedSSRFHosts = map[string]string{
		"instance-data":            "explicit metadata hostname",
		"metadata.google.internal": "explicit metadata hostname",
	}

	blockedSSRFRules = []ssrfRule{
		{prefix: netip.MustParsePrefix("127.0.0.0/8"), label: "loopback"},
		{prefix: netip.MustParsePrefix("10.0.0.0/8"), label: "private"},
		{prefix: netip.MustParsePrefix("172.16.0.0/12"), label: "private"},
		{prefix: netip.MustParsePrefix("192.168.0.0/16"), label: "private"},
		{prefix: netip.MustParsePrefix("169.254.0.0/16"), label: "link-local"},
		{prefix: netip.MustParsePrefix("224.0.0.0/4"), label: "multicast"},
		{prefix: netip.MustParsePrefix("255.255.255.255/32"), label: "broadcast"},
		{prefix: netip.MustParsePrefix("0.0.0.0/8"), label: "unspecified"},
		{prefix: netip.MustParsePrefix("::1/128"), label: "loopback"},
		{prefix: netip.MustParsePrefix("fe80::/10"), label: "link-local"},
		{prefix: netip.MustParsePrefix("fc00::/7"), label: "unique local"},
	}
)

// IsSSRFTarget returns whether host resolves to a blocked SSRF destination.
func IsSSRFTarget(host string) (bool, string, error) {
	normalizedHost := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if normalizedHost == "" {
		err := fmt.Errorf("lookup ip: empty host")
		return true, "dns resolution failed", err
	}
	if reason, ok := blockedSSRFHosts[normalizedHost]; ok {
		return true, reason, nil
	}

	ips, err := lookupIP(normalizedHost)
	if err != nil {
		return true, "dns resolution failed", err
	}
	if len(ips) == 0 {
		err = fmt.Errorf("lookup ip %s: no addresses", normalizedHost)
		return true, "dns resolution failed", err
	}

	for _, ip := range ips {
		if blocked, reason := blockedSSRFIP(ip); blocked {
			return true, reason, nil
		}
	}

	return false, "", nil
}

func blockedSSRFIP(ip net.IP) (bool, string) {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false, ""
	}
	addr = addr.Unmap()

	for _, rule := range blockedSSRFRules {
		if rule.prefix.Contains(addr) {
			return true, fmt.Sprintf("resolved to %s address %s", rule.label, addr.String())
		}
	}

	return false, ""
}
