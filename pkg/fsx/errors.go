package fsx

import (
	"github.com/joomcode/errorx"
	"strconv"
)
import "golang.hedera.com/solo-provisioner/pkg/security/principal"

var (
	ErrorsNamespace       = errorx.NewNamespace("fsx")
	FileAlreadyExists     = ErrorsNamespace.NewType("file_already_exists")
	FileNotFound          = ErrorsNamespace.NewType("file_not_found")
	FileSystemError       = ErrorsNamespace.NewType("filesystem_error")
	FileTypeError         = ErrorsNamespace.NewType("file_type_error")
	OwnershipChangeError  = ErrorsNamespace.NewType("ownership_change_error")
	PermissionChangeError = ErrorsNamespace.NewType("permission_change_error")

	pathProperty      = errorx.RegisterPrintableProperty("path")
	userProperty      = errorx.RegisterPrintableProperty("user")
	groupProperty     = errorx.RegisterPrintableProperty("group")
	recursiveProperty = errorx.RegisterPrintableProperty("recursive")
	permsProperty     = errorx.RegisterPrintableProperty("perms")
)

const (
	ownershipChangeErrorMsg  string = "failed to change file or directory ownership [ path = '%s', user = '%s', group = '%s', recursive = '%t' ]"
	permissionChangeErrorMsg        = "failed to change file or directory permissions [ path = '%s', perms = '%s', recursive = '%t' ]"
)

func NewOwnershipChangeError(cause error, path string, user principal.User, group principal.Group, recursive bool) *errorx.Error {
	if cause == nil {
		return OwnershipChangeError.New(ownershipChangeErrorMsg, path, user.Name(), group.Name(), recursive).
			WithProperty(pathProperty, path).
			WithProperty(userProperty, user.Name()).
			WithProperty(groupProperty, group.Name()).
			WithProperty(recursiveProperty, recursive)
	}

	return OwnershipChangeError.New(ownershipChangeErrorMsg, path, user.Name(), group.Name(), recursive).
		WithProperty(pathProperty, path).
		WithProperty(userProperty, user.Name()).
		WithProperty(groupProperty, group.Name()).
		WithProperty(recursiveProperty, recursive).
		WithUnderlyingErrors(cause)
}

func NewPermissionChangeError(cause error, path string, perms uint, recursive bool) *errorx.Error {
	if cause == nil {
		return PermissionChangeError.New(permissionChangeErrorMsg, path, strconv.FormatUint(uint64(perms), 8), recursive).
			WithProperty(pathProperty, path).
			WithProperty(permsProperty, perms).
			WithProperty(recursiveProperty, recursive)
	}

	return PermissionChangeError.New(permissionChangeErrorMsg, path, strconv.FormatUint(uint64(perms), 8), recursive).
		WithProperty(pathProperty, path).
		WithProperty(permsProperty, perms).
		WithProperty(recursiveProperty, recursive).
		WithUnderlyingErrors(cause)
}

// SafeErrorDetails emits a PII-safe slice.
func SafeErrorDetails(err *errorx.Error) []string {
	var safeDetails []string
	if err == nil {
		return safeDetails
	}

	for _, prop := range []errorx.Property{pathProperty, userProperty, groupProperty, permsProperty, recursiveProperty} {
		if val, ok := err.Property(prop); ok {
			switch prop {
			case pathProperty:
				safeDetails = append(safeDetails, val.(string))
			case userProperty:
				safeDetails = append(safeDetails, val.(string))
			case groupProperty:
				safeDetails = append(safeDetails, val.(string))
			case recursiveProperty:
				safeDetails = append(safeDetails, strconv.FormatBool(val.(bool)))
			case permsProperty:
				safeDetails = append(safeDetails, strconv.FormatUint(uint64(val.(uint)), 8))
			}
		}
	}

	return safeDetails
}
