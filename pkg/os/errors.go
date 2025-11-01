package os

import "github.com/joomcode/errorx"

var (
	ErrNamespace = errorx.NewNamespace("os")

	SwapErrTrait    = errorx.RegisterTrait("swap_error")
	FileErrTrait    = errorx.RegisterTrait("file_error")
	SystemdErrTrait = errorx.RegisterTrait("systemd_error")

	ErrSwapOutOfMemory    = ErrNamespace.NewType("out_of_memory", SwapErrTrait)
	ErrInvalidSwapFile    = ErrNamespace.NewType("invalid_swap_file", SwapErrTrait)
	ErrSwapUnknownSyscall = ErrNamespace.NewType("unknown_syscall", SwapErrTrait)
	ErrNonSyscall         = ErrNamespace.NewType("non_syscall", SwapErrTrait)
	ErrSwapNotSuperUser   = ErrNamespace.NewType("not_super_user", SwapErrTrait)
	ErrFileInaccessible   = ErrNamespace.NewType("file_inaccessible", FileErrTrait)
	ErrSwapDeviceNotFound = ErrNamespace.NewType("device_not_found", SwapErrTrait)
	ErrFileRead           = ErrNamespace.NewType("file_read_error", FileErrTrait)
	ErrFileWrite          = ErrNamespace.NewType("file_write_error", FileErrTrait)
	ErrSystemdConnection  = ErrNamespace.NewType("systemd_connection_error", SystemdErrTrait)
	ErrSystemdOperation   = ErrNamespace.NewType("systemd_operation_error", SystemdErrTrait)

	PathProperty         = errorx.RegisterProperty("path")
	SysErrorCodeProperty = errorx.RegisterProperty("sys_error_code")
)
