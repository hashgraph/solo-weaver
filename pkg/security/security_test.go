// SPDX-License-Identifier: Apache-2.0

package security

import (
	"testing"
)

func TestServiceAccountDefaults(t *testing.T) {
	if ServiceAccountUserName() != "hedera" {
		t.Errorf("expected default username 'hedera', got %q", ServiceAccountUserName())
	}
	if ServiceAccountUserId() != "2000" {
		t.Errorf("expected default user id '2000', got %q", ServiceAccountUserId())
	}
	if ServiceAccountGroupName() != "hedera" {
		t.Errorf("expected default group name 'hedera', got %q", ServiceAccountGroupName())
	}
	if ServiceAccountGroupId() != "2000" {
		t.Errorf("expected default group id '2000', got %q", ServiceAccountGroupId())
	}
}

func TestSetServiceAccount(t *testing.T) {
	acc := ServiceAccount{
		UserName:  "testuser",
		UserId:    "1234",
		GroupName: "testgroup",
		GroupId:   "5678",
	}
	SetServiceAccount(acc)

	if ServiceAccountUserName() != "testuser" {
		t.Errorf("expected username 'testuser', got %q", ServiceAccountUserName())
	}
	if ServiceAccountUserId() != "1234" {
		t.Errorf("expected user id '1234', got %q", ServiceAccountUserId())
	}
	if ServiceAccountGroupName() != "testgroup" {
		t.Errorf("expected group name 'testgroup', got %q", ServiceAccountGroupName())
	}
	if ServiceAccountGroupId() != "5678" {
		t.Errorf("expected group id '5678', got %q", ServiceAccountGroupId())
	}
}
