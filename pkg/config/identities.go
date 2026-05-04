// SPDX-License-Identifier: Apache-2.0

package config

import "github.com/hashgraph/solo-weaver/pkg/security"

var weaverUser = security.SystemUser{
	UserName:  "weaver",
	UserId:    "2500",
	GroupName: "weaver",
	GroupId:   "2500",
}

var hederaUser = security.SystemUser{
	UserName:  "hedera",
	UserId:    "2000",
	GroupName: "hedera",
	GroupId:   "2000",
}

func WeaverUser() security.SystemUser { return weaverUser }
func WeaverUserName() string          { return weaverUser.UserName }
func WeaverUserId() string            { return weaverUser.UserId }
func WeaverGroupName() string         { return weaverUser.GroupName }
func WeaverGroupId() string           { return weaverUser.GroupId }
func WeaverHomeDir() string           { return "/home/weaver" }

func HederaUser() security.SystemUser { return hederaUser }
func HederaUserName() string          { return hederaUser.UserName }
func HederaUserId() string            { return hederaUser.UserId }
func HederaGroupName() string         { return hederaUser.GroupName }
func HederaGroupId() string           { return hederaUser.GroupId }
