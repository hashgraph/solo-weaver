package hardware

type Spec interface {
	//Cpu(cpu ) boolean
	//Memory(mem ghw.Memory) int64
	Disk(size int64) int64
}
