package hardware

import "github.com/zcalusic/sysinfo"

type Spec interface {
	//Cpu(cpu ) boolean
	//Memory(mem ghw.Memory) int64
	Disk(size int64) int64
	Check(si sysinfo.SysInfo) (bool, error)
}
