package software

const (
	iptablesHash    = "HASH_PLACEHOLDER"
	iptablesVersion = "1.8.7"
	iptablesURL     = "/%s/iptables-%s.tar.bz2"
)

type iptables struct {
	*packageInstaller
}

func NewIptables() Package {
	sp := &iptables{
		packageInstaller: newPackageInstaller(
			WithPackageUrl(iptablesURL),
			WithPackageHash(iptablesHash),
			WithPackageVersion(iptablesVersion),
		),
	}

	return sp
}
