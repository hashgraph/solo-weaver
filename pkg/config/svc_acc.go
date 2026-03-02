// SPDX-License-Identifier: Apache-2.0

package config

import "github.com/hashgraph/solo-weaver/pkg/security"

var (
	svcAcc = security.ServiceAccount{
		UserName:  "weaver",
		UserId:    "2500",
		GroupName: "weaver",
		GroupId:   "2500",
	}
)

func ServiceAccount() security.ServiceAccount {
	return svcAcc
}
