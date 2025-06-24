package security

import "os"

const (
	ServiceAccountUserName      = "hedera"          // the required service account username.
	ServiceAccountUserId        = "2000"            // the required service account id.
	ServiceAccountGroupName     = "hedera"          // the required service account primary group name.
	ServiceAccountGroupId       = "2000"            // the required service account primary group id.
	ACLFolderPerms              = os.FileMode(0755) // the ACL folder permissions.
	ACLFilePerms                = os.FileMode(0755) // the ACL file permissions.
	ACLUserROPerms              = os.FileMode(0400) // the ACL user read-only permissions.
	ACLUserRWGroupOtherROPerms  = os.FileMode(0644) // the ACL user read-write permissions but read-only for group and other.
	ACLUserRWXGroupOtherRXPerms = os.FileMode(0755) // the ACL user read-write-execute permissions but read-execute for group and other.
)
