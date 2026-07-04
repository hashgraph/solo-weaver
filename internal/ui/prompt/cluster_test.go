// SPDX-License-Identifier: Apache-2.0

package prompt

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateMgmtCIDRs(t *testing.T) {
	require.NoError(t, validateMgmtCIDRs(""))                           // empty allowed (skip firewall)
	require.NoError(t, validateMgmtCIDRs("10.0.0.0/8"))                 // single
	require.NoError(t, validateMgmtCIDRs("10.0.0.0/8, 192.168.0.0/16")) // spaced list
	require.NoError(t, validateMgmtCIDRs("10.0.0.0/8,"))                // trailing comma tolerated
	require.Error(t, validateMgmtCIDRs("10.0.0.0"))                     // missing prefix
	require.Error(t, validateMgmtCIDRs("not-a-cidr"))
	require.Error(t, validateMgmtCIDRs("2001:db8::/32")) // IPv6 not supported by the inet host table
}

func TestValidatePodCIDR(t *testing.T) {
	require.NoError(t, validatePodCIDR("")) // empty omits the rule
	require.NoError(t, validatePodCIDR("10.4.0.0/14"))
	require.Error(t, validatePodCIDR("10.4.0.0"))
	require.Error(t, validatePodCIDR("garbage"))
	require.Error(t, validatePodCIDR("2001:db8::/32")) // IPv6 not supported by the inet host table
}

func TestValidateSSHPort(t *testing.T) {
	require.NoError(t, validateSSHPort("22"))
	require.NoError(t, validateSSHPort("2222"))
	require.Error(t, validateSSHPort(""))      // required
	require.Error(t, validateSSHPort("0"))     // out of range
	require.Error(t, validateSSHPort("70000")) // out of range
	require.Error(t, validateSSHPort("abc"))
}

func TestValidateInClusterPorts(t *testing.T) {
	require.NoError(t, validateInClusterPorts("")) // empty allowed
	require.NoError(t, validateInClusterPorts("6443,4244,7472,10250"))
	require.NoError(t, validateInClusterPorts("6443, 10250")) // spaced
	require.Error(t, validateInClusterPorts("6443,99999"))
	require.Error(t, validateInClusterPorts("6443,abc"))
}

func TestParsePortList(t *testing.T) {
	got, err := ParsePortList("6443, 4244 ,10250,")
	require.NoError(t, err)
	require.Equal(t, []int{6443, 4244, 10250}, got)

	empty, err := ParsePortList("")
	require.NoError(t, err)
	require.Empty(t, empty)

	_, err = ParsePortList("6443,abc")
	require.Error(t, err)
}
