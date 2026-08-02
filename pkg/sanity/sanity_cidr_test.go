// SPDX-License-Identifier: Apache-2.0

package sanity

import (
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
