package os

import "github.com/joomcode/errorx"

var (
	ErrNamespace = errorx.NewNamespace("os")

	SwapErrTrait = errorx.RegisterTrait("swap_error")
	FileErrTrait = errorx.RegisterTrait("file_error")

	ErrSwapOutOfMemory    = ErrNamespace.NewType("out_of_memory", SwapErrTrait)
	ErrSwapUnknownSyscall = ErrNamespace.NewType("unknown_syscall_error", SwapErrTrait)
	ErrNonSyscallError    = ErrNamespace.NewType("non_syscall_error", SwapErrTrait)
	ErrSwapNotSuperUser   = ErrNamespace.NewType("not_super_user", SwapErrTrait)
	ErrFileInaccessible   = ErrNamespace.NewType("file_inaccessible", FileErrTrait)
	ErrFileRead           = ErrNamespace.NewType("file_read_error", FileErrTrait)

	PathProperty         = errorx.RegisterProperty("path")
	SysErrorCodeProperty = errorx.RegisterProperty("sys_error_code")
)
