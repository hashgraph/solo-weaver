package core

type Provider interface {
	NewBareMetalSetupManager() BareMetalSetupManager
	NewKubernetesSetupManager() KubernetesSetupManager
}
