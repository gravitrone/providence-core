package hooks

import (
	"fmt"
	"net"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	originalLookupIP := lookupIP
	lookupIP = func(host string) ([]net.IP, error) {
		switch host {
		case "127.0.0.1", "::1":
			return []net.IP{net.ParseIP("1.1.1.1")}, nil
		default:
			return originalLookupIP(host)
		}
	}

	code := m.Run()
	lookupIP = originalLookupIP
	os.Exit(code)
}

func TestIsSSRFTarget(t *testing.T) {
	originalLookupIP := lookupIP
	lookupIP = func(host string) ([]net.IP, error) {
		addresses := map[string][]string{
			"0.0.0.0":               {"0.0.0.0"},
			"1.1.1.1":               {"1.1.1.1"},
			"10.0.0.1":              {"10.0.0.1"},
			"127.0.0.1":             {"127.0.0.1"},
			"169.254.169.254":       {"169.254.169.254"},
			"172.16.0.1":            {"172.16.0.1"},
			"192.168.1.10":          {"192.168.1.10"},
			"224.0.0.1":             {"224.0.0.1"},
			"255.255.255.255":       {"255.255.255.255"},
			"8.8.8.8":               {"8.8.8.8"},
			"::1":                   {"::1"},
			"api.anthropic.com":     {"104.18.33.45", "104.18.32.45"},
			"fc00::1":               {"fc00::1"},
			"fe80::1":               {"fe80::1"},
			"github.com":            {"140.82.121.4"},
			"localhost.evil.com":    {"127.0.0.1"},
			"mixed.example.com":     {"1.1.1.1", "127.0.0.1"},
			"public.example.com":    {"1.1.1.1", "8.8.8.8"},
			"safe.ipv6.example.com": {"2001:4860:4860::8888"},
		}

		rawIPs, ok := addresses[host]
		if !ok {
			return nil, fmt.Errorf("lookup %s: no such host", host)
		}

		ips := make([]net.IP, 0, len(rawIPs))
		for _, rawIP := range rawIPs {
			ip := net.ParseIP(rawIP)
			if ip == nil {
				return nil, fmt.Errorf("parse ip %s", rawIP)
			}
			ips = append(ips, ip)
		}
		return ips, nil
	}
	t.Cleanup(func() {
		lookupIP = originalLookupIP
	})

	tests := []struct {
		name        string
		host        string
		wantBlocked bool
		wantReason  string
		wantErr     bool
	}{
		{name: "blocks IPv4 loopback", host: "127.0.0.1", wantBlocked: true, wantReason: "loopback"},
		{name: "blocks IPv4 private 10", host: "10.0.0.1", wantBlocked: true, wantReason: "private"},
		{name: "blocks IPv4 private 172", host: "172.16.0.1", wantBlocked: true, wantReason: "private"},
		{name: "blocks IPv4 private 192", host: "192.168.1.10", wantBlocked: true, wantReason: "private"},
		{name: "blocks IPv4 link local", host: "169.254.169.254", wantBlocked: true, wantReason: "link-local"},
		{name: "blocks IPv4 multicast", host: "224.0.0.1", wantBlocked: true, wantReason: "multicast"},
		{name: "blocks IPv4 broadcast", host: "255.255.255.255", wantBlocked: true, wantReason: "broadcast"},
		{name: "blocks IPv4 unspecified", host: "0.0.0.0", wantBlocked: true, wantReason: "unspecified"},
		{name: "blocks IPv6 loopback", host: "::1", wantBlocked: true, wantReason: "loopback"},
		{name: "blocks IPv6 link local", host: "fe80::1", wantBlocked: true, wantReason: "link-local"},
		{name: "blocks IPv6 unique local", host: "fc00::1", wantBlocked: true, wantReason: "unique local"},
		{name: "blocks explicit metadata hostname", host: "metadata.google.internal", wantBlocked: true, wantReason: "explicit metadata hostname"},
		{name: "blocks explicit metadata hostname case insensitive", host: "METADATA.GOOGLE.INTERNAL.", wantBlocked: true, wantReason: "explicit metadata hostname"},
		{name: "blocks explicit instance data hostname", host: "instance-data", wantBlocked: true, wantReason: "explicit metadata hostname"},
		{name: "blocks hostname resolving to loopback", host: "localhost.evil.com", wantBlocked: true, wantReason: "loopback"},
		{name: "blocks hostname when any resolved address is blocked", host: "mixed.example.com", wantBlocked: true, wantReason: "loopback"},
		{name: "allows public IPv4", host: "1.1.1.1", wantBlocked: false},
		{name: "allows second public IPv4", host: "8.8.8.8", wantBlocked: false},
		{name: "allows github hostname", host: "github.com", wantBlocked: false},
		{name: "allows anthropic hostname", host: "api.anthropic.com", wantBlocked: false},
		{name: "allows hostname with only public IPs", host: "public.example.com", wantBlocked: false},
		{name: "allows public IPv6", host: "safe.ipv6.example.com", wantBlocked: false},
		{name: "returns dns failure as blocked", host: "missing.invalid", wantBlocked: true, wantReason: "dns resolution failed", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocked, reason, err := IsSSRFTarget(tt.host)

			assert.Equal(t, tt.wantBlocked, blocked)
			if tt.wantReason == "" {
				assert.Empty(t, reason)
			} else {
				assert.Contains(t, reason, tt.wantReason)
			}

			if tt.wantErr {
				require.Error(t, err)
				assert.Equal(t, "dns resolution failed", reason)
				return
			}

			require.NoError(t, err)
		})
	}
}
