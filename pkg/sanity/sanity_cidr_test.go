// SPDX-License-Identifier: Apache-2.0

package sanity

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSanity_ValidateCIDR(t *testing.T) {
	testCases := []struct {
		name        string
		cidr        string
		expectError bool
	}{
		// Valid
		{name: "ipv4 /8", cidr: "10.0.0.0/8", expectError: false},
		{name: "ipv4 /24", cidr: "192.168.1.0/24", expectError: false},
		{name: "ipv4 /32 host", cidr: "203.0.113.5/32", expectError: false},
		{name: "ipv6 /32", cidr: "2001:db8::/32", expectError: false},
		{name: "ipv6 /128 host", cidr: "2001:db8::1/128", expectError: false},

		// Invalid
		{name: "empty", cidr: "", expectError: true},
		{name: "bare ipv4 no mask", cidr: "10.0.0.0", expectError: true},
		{name: "bare ipv6 no mask", cidr: "2001:db8::1", expectError: true},
		{name: "mask out of range", cidr: "10.0.0.0/33", expectError: true},
		{name: "not an address", cidr: "not-a-cidr", expectError: true},
		{name: "octet overflow", cidr: "10.0.0.256/24", expectError: true},
		{name: "shell metachar injection", cidr: "10.0.0.0/8;reboot", expectError: true},
		{name: "command substitution", cidr: "10.0.0.0/$(id)", expectError: true},
		{name: "newline", cidr: "10.0.0.0/8\n", expectError: true},
		{name: "leading space", cidr: " 10.0.0.0/8", expectError: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateCIDR(tc.cidr)
			if tc.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSanity_CIDRIsIPv6(t *testing.T) {
	testCases := []struct {
		name    string
		cidr    string
		wantV6  bool
		wantErr bool
	}{
		{name: "ipv4", cidr: "10.0.0.0/8", wantV6: false},
		{name: "ipv4 host", cidr: "192.168.68.117/32", wantV6: false},
		{name: "ipv6", cidr: "2001:db8::/32", wantV6: true},
		{name: "ipv6 host", cidr: "2001:db8::1/128", wantV6: true},
		{name: "ipv4-mapped ipv6 classifies as ipv4", cidr: "::ffff:10.0.0.1/128", wantV6: false},
		{name: "not a cidr", cidr: "not-a-cidr", wantErr: true},
		{name: "bare ip", cidr: "10.0.0.1", wantErr: true},
		{name: "empty", cidr: "", wantErr: true},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			isV6, err := CIDRIsIPv6(tc.cidr)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantV6, isV6)
		})
	}
}

func TestSanity_ValidatePort(t *testing.T) {
	testCases := []struct {
		name        string
		port        string
		expectError bool
	}{
		// Valid
		{name: "low boundary 1", port: "1", expectError: false},
		{name: "kubelet 10250", port: "10250", expectError: false},
		{name: "high boundary 65535", port: "65535", expectError: false},

		// Invalid
		{name: "empty", port: "", expectError: true},
		{name: "zero", port: "0", expectError: true},
		{name: "above range", port: "65536", expectError: true},
		{name: "negative", port: "-1", expectError: true},
		{name: "non-numeric", port: "ssh", expectError: true},
		{name: "trailing junk", port: "443x", expectError: true},
		{name: "shell metachar", port: "443;reboot", expectError: true},
		{name: "whitespace", port: " 443", expectError: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePort(tc.port)
			if tc.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSanity_ValidateFQDN(t *testing.T) {
	testCases := []struct {
		name        string
		fqdn        string
		expectError bool
	}{
		// Valid
		{name: "two labels", fqdn: "example.com", expectError: false},
		{name: "three labels", fqdn: "jump.corp.example.com", expectError: false},
		{name: "trailing root dot", fqdn: "jump.corp.example.com.", expectError: false},
		{name: "internal hyphen", fqdn: "jump-01.corp.example.com", expectError: false},
		{name: "digits in label", fqdn: "node1.example.com", expectError: false},
		{name: "uppercase", fqdn: "Jump.Example.COM", expectError: false},
		{name: "63-byte label", fqdn: strings.Repeat("a", 63) + ".example.com", expectError: false},

		// Structure
		{name: "empty", fqdn: "", expectError: true},
		{name: "root dot only", fqdn: ".", expectError: true},
		// A bare hostname would resolve differently per host search domain.
		{name: "dotless hostname", fqdn: "localhost", expectError: true},
		{name: "empty label", fqdn: "jump..example.com", expectError: true},
		{name: "leading dot", fqdn: ".example.com", expectError: true},
		{name: "double trailing dot", fqdn: "example.com..", expectError: true},
		{name: "leading hyphen", fqdn: "-jump.example.com", expectError: true},
		{name: "trailing hyphen", fqdn: "jump-.example.com", expectError: true},
		{name: "64-byte label", fqdn: strings.Repeat("a", 64) + ".example.com", expectError: true},
		{name: "254-byte name", fqdn: strings.Repeat("a.", 127) + "example.com", expectError: true},

		// An IP must keep producing ValidateCIDR's "explicit prefix length" error
		// rather than being handed to a resolver.
		{name: "bare IPv4", fqdn: "10.0.0.1", expectError: true},
		{name: "bare IPv6", fqdn: "2001:db8::1", expectError: true},
		{name: "all-digit final label", fqdn: "999.1.2.3", expectError: true},

		// Character set. shellMetachars does not cover several of these, which is
		// why the validator is an allowlist rather than a denylist.
		{name: "underscore", fqdn: "jump_host.example.com", expectError: true},
		{name: "space", fqdn: "jump host.example.com", expectError: true},
		{name: "slash", fqdn: "jump.example.com/32", expectError: true},
		{name: "colon", fqdn: "jump.example.com:22", expectError: true},
		{name: "at sign", fqdn: "root@jump.example.com", expectError: true},
		{name: "percent", fqdn: "jump%00.example.com", expectError: true},
		{name: "hash", fqdn: "jump#.example.com", expectError: true},
		{name: "bang", fqdn: "jump!.example.com", expectError: true},
		{name: "single quote", fqdn: "jump'.example.com", expectError: true},
		{name: "double quote", fqdn: "jump\".example.com", expectError: true},
		{name: "backslash", fqdn: "jump\\.example.com", expectError: true},
		{name: "non-ASCII homograph", fqdn: "jumр.example.com", expectError: true},

		// Injection and traversal.
		{name: "shell metachar", fqdn: "example.com;reboot", expectError: true},
		{name: "command substitution", fqdn: "example.com$(id)", expectError: true},
		{name: "backtick", fqdn: "example.com`id`", expectError: true},
		{name: "nft injection", fqdn: "example.com }; drop; set x {", expectError: true},
		{name: "path traversal", fqdn: "../../etc/passwd", expectError: true},
		{name: "sql-ish", fqdn: "example.com' OR '1'='1", expectError: true},

		// Control bytes.
		{name: "nul", fqdn: "example.com\x00", expectError: true},
		{name: "newline", fqdn: "example.com\n", expectError: true},
		{name: "carriage return", fqdn: "example.com\r", expectError: true},
		{name: "tab", fqdn: "example.com\t", expectError: true},
		{name: "bell", fqdn: "example.com\x07", expectError: true},
		{name: "escape", fqdn: "example.com\x1b", expectError: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateFQDN(tc.fqdn)
			if tc.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSanity_ValidateIPv4CIDROrFQDN(t *testing.T) {
	testCases := []struct {
		name        string
		entry       string
		expectError bool
	}{
		{name: "ipv4 cidr", entry: "10.0.0.0/8", expectError: false},
		{name: "single host cidr", entry: "192.0.2.7/32", expectError: false},
		{name: "fqdn", entry: "jump.corp.example.com", expectError: false},

		{name: "empty", entry: "", expectError: true},
		// The flag-shaped layer is IPv4-only, matching ValidateIPv4CIDR.
		{name: "ipv6 cidr", entry: "2001:db8::/32", expectError: true},
		{name: "maskless ipv4", entry: "10.0.0.1", expectError: true},
		{name: "dotless hostname", entry: "localhost", expectError: true},
		{name: "fqdn with mask", entry: "jump.example.com/32", expectError: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateIPv4CIDROrFQDN(tc.entry)
			if tc.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
