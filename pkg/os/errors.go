package os

import "github.com/joomcode/errorx"

var (
	ErrNamespace = errorx.NewNamespace("os")

	SwapErrTrait = errorx.RegisterTrait("swap_error")
	FileErrTrait = errorx.RegisterTrait("file_error")

	ErrSwapOutOfMemory    = ErrNamespace.NewType("out_of_memory", SwapErrTrait)
	ErrSwapUnknownSyscall = ErrNamespace.NewType("unknown_syscall", SwapErrTrait)
	ErrNonSyscall         = ErrNamespace.NewType("non_syscall", SwapErrTrait)
	ErrSwapNotSuperUser   = ErrNamespace.NewType("not_super_user", SwapErrTrait)
	ErrFileInaccessible   = ErrNamespace.NewType("file_inaccessible", FileErrTrait)
	ErrSwapDeviceNotFound = ErrNamespace.NewType("device_not_found", SwapErrTrait)
	ErrFileRead           = ErrNamespace.NewType("file_read_error", FileErrTrait)
	ErrFileWrite          = ErrNamespace.NewType("file_write_error", FileErrTrait)

	PathProperty         = errorx.RegisterProperty("path")
	SysErrorCodeProperty = errorx.RegisterProperty("sys_error_code")
)
