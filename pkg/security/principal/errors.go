// SPDX-License-Identifier: Apache-2.0

package principal

import (
	"github.com/joomcode/errorx"
)

var (
	ErrorsNamespace    = errorx.NewNamespace("security")
	UserNotFoundError  = ErrorsNamespace.NewType("user_not_found_*errorx.Error")
	GroupNotFoundError = ErrorsNamespace.NewType("group_not_found_*errorx.Error")
)

var (
	nameProperty = errorx.RegisterPrintableProperty("name")
	uidProperty  = errorx.RegisterPrintableProperty("uid")
	gidProperty  = errorx.RegisterPrintableProperty("gid")
)

func NewUserNotFoundError(cause error, name string, uid string) *errorx.Error {
	if cause == nil {
		return UserNotFoundError.New("User with name %q and uid %q not found!", name, uid).
			WithProperty(nameProperty, name).
			WithProperty(uidProperty, uid)
	}

	return UserNotFoundError.New("User with name %q and uid %q not found!", name, uid).
		WithProperty(nameProperty, name).
		WithProperty(uidProperty, uid).
		WithUnderlyingErrors(cause)
}

func NewGroupNotFoundError(cause error, name string, gid string) *errorx.Error {
	if cause == nil {
		return GroupNotFoundError.New("Group with name %q and gid %q not found!", name, gid).
			WithProperty(nameProperty, name).
			WithProperty(gidProperty, gid)
	}

	return GroupNotFoundError.New("Group with name %q and gid %q not found!", name, gid).
		WithProperty(nameProperty, name).
		WithProperty(gidProperty, gid).
		WithUnderlyingErrors(cause)
}

// SafeErrorDetails emits a PII-safe slice.
func SafeErrorDetails(err *errorx.Error) []string {
	var safeDetails []string
	if err == nil {
		return safeDetails
	}

	for _, prop := range []errorx.Property{nameProperty, uidProperty, gidProperty} {
		if val, ok := err.Property(prop); ok {
			safeDetails = append(safeDetails, val.(string))
		}
	}

	return safeDetails
}
