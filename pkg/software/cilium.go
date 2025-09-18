package software

const ciliumCliHash = "HASH_PLACEHOLDER"
const ciliumCliVersion = "1.14.4"
const ciliumCliURL = "/%s/cilium-%s.tar.gz"

type cilium struct{}

func NewCilium(opts ...Option) Package {
	return &cilium{}
}
