//go:build !darwin && !windows

package core

type providerUnix struct{}

func (p *providerUnix) NewBareMetalSetupManager() BareMetalSetupManager {
	return NewUnixSetupManager()
}

func (p *providerUnix) NewKubernetesSetupManager() KubernetesSetupManager {
	return NewUnixKubernetesSetupManager()
}

func NewProvider() Provider {
	return &providerUnix{}
}
