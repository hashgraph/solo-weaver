package security

import "os"

const (
	ACLFolderPerms              = os.FileMode(0755) // the ACL folder permissions.
	ACLFilePerms                = os.FileMode(0755) // the ACL file permissions.
	ACLUserROPerms              = os.FileMode(0400) // the ACL user read-only permissions.
	ACLUserRWGroupOtherROPerms  = os.FileMode(0644) // the ACL user read-write permissions but read-only for group and other.
	ACLUserRWXGroupOtherRXPerms = os.FileMode(0755) // the ACL user read-write-execute permissions but read-execute for group and other.
)

var (
	serviceAccountUserName  = "hedera" // the required service account username.
	serviceAccountUserId    = "2000"   // the required service account id.
	serviceAccountGroupName = "hedera" // the required service account primary group name.
	serviceAccountGroupId   = "2000"   // the required service account primary group id.
)

type ServiceAccount struct {
	UserName  string
	UserId    string
	GroupName string
	GroupId   string
}

func SetServiceAccount(svcAcc ServiceAccount) {
	if svcAcc.UserName != "" {
		serviceAccountUserName = svcAcc.UserName
	}
	if svcAcc.UserId != "" {
		serviceAccountUserId = svcAcc.UserId
	}
	if svcAcc.GroupName != "" {
		serviceAccountGroupName = svcAcc.GroupName
	}
	if svcAcc.GroupId != "" {
		serviceAccountGroupId = svcAcc.GroupId
	}
}

func ServiceAccountUserName() string {
	return serviceAccountUserName
}

func ServiceAccountUserId() string {
	return serviceAccountUserId
}

func ServiceAccountGroupName() string {
	return serviceAccountGroupName
}

func ServiceAccountGroupId() string {
	return serviceAccountGroupId
}
