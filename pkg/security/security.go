// SPDX-License-Identifier: Apache-2.0

package security

import "os"

const (
	ACLFolderPerms              = os.FileMode(0755)
	ACLFilePerms                = os.FileMode(0755)
	ACLUserROPerms              = os.FileMode(0400)
	ACLUserRWGroupOtherROPerms  = os.FileMode(0644)
	ACLUserRWXGroupOtherRXPerms = os.FileMode(0755)
)

// SystemUser holds the identity (user + group) for a Linux system account.
type SystemUser struct {
	UserName  string
	UserId    string
	GroupName string
	GroupId   string
}
