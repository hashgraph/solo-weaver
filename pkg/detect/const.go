package detect

const (
	// smallSystemMemReserve denotes 5% reserve of the total physical memory
	// for smaller systems we need to allocate a bit more in terms of percentage of the total memory
	smallSystemMemReserve = 0.05

	// largeSystemMemReserve denotes 2% reserve of the total physical memory
	// for larger systems 2% is large enough
	largeSystemMemReserve = 0.02

	// smallSystemMaxMemSize defines the lowest threshold that defines if a system is small
	smallSystemMaxMemSize = 34359738368 // 34Gb

	// DefaultMemBufferPercent defines the amount of memory buffer we allocate for docker containers
	defaultMemBufferPercent = 0.5 // 50%

)

var (
	javaMemoryUnits = []string{"b", "k", "m", "g", "t", "p", "e"}

	// release ID to flavor
	linuxFlavorMapping = map[string]string{
		"ubuntu": OSFlavorLinuxUbuntu,
		"fedora": OSFlavorLinuxFedora,
		"debian": OSFlavorLinuxDebian,
		"centos": OSFlavorLinuxCentos,
		"rhel":   OSFlavorLinuxRhel,
		"sles":   OSFlavorLinuxSuse,
		"ol":     OSFlavorLinuxOracle,
	}

	// release version to flavor
	macFlavorMapping = map[string]string{
		"10.12.*": OSFlavorMacSierra,
		"10.13.*": OSFlavorMacHighSierra,
		"10.14.*": OSFlavorMacMojave,
		"10.15.*": OSFlavorMacCatalina,
		"11.*":    OSFlavorMacBigSur,
		"12.*":    OSFlavorMacMonterey,
		"13.*":    OSFlavorMacMojave,
	}
)

const (
	RedhatReleaseFileName = "redhat-release"
	LSBReleaseFileName    = "lsb-release"
	OSReleaseFileName     = "os-release"

	EtcRedhatReleasePath = "/etc/redhat-release"
	EtcLSBReleasePath    = "/etc/lsb-release"
	EtcOSReleasePath     = "/etc/os-release"

	// RedhatVersionRegex contains regex for semver version and codename such as "8.7 (Oopta)"
	RedhatVersionRegex = "([0-9]+)\\.?([0-9]+)*\\.?([0-9]+)*[-_]?([a-zA-Z0-9\\.]+)*\\s?[(]([a-zA-Z0-9\\.]+)*\\s?[)]"

	OSFlavorUnknown  = "unknown"
	OSVersionUnknown = "unknown"

	OSFlavorLinuxRhel   = "rhel"
	OSFlavorLinuxUbuntu = "ubuntu"
	OSFlavorLinuxDebian = "debian"
	OSFlavorLinuxFedora = "fedora"
	OSFlavorLinuxSuse   = "suse"
	OSFlavorLinuxOracle = "oracle"
	OSFlavorLinuxCentos = "centos"

	OSFlavorMacSierra     = "sierra"
	OSFlavorMacHighSierra = "high-sierra"
	OSFlavorMacMojave     = "mojave"
	OSFlavorMacCatalina   = "catalina"
	OSFlavorMacBigSur     = "big-sur"
	OSFlavorMacMonterey   = "monterey"
	OSFlavorMacVentura    = "ventura"
)
