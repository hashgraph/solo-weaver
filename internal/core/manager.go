package core

import "context"

type BareMetalSetupManager interface {
	SetupDirectories(ctx context.Context) error
	UpdateOS(ctx context.Context) error
	UpgradeOS(ctx context.Context) error
	DisableSwap(ctx context.Context) error
	InstallIpTables(ctx context.Context) error
	InstallGPG(ctx context.Context) error
	InstallCurl(ctx context.Context) error
	InstallConntrack(ctx context.Context) error
	InstallEBTables(ctx context.Context) error
	InstallSoCat(ctx context.Context) error
	InstallNFTables(ctx context.Context) error
	InstallKernelModules(ctx context.Context) error
	RemoveContainerd(ctx context.Context) error
	RemoveUnusedPackages(ctx context.Context) error
	CheckHardware(ctx context.Context) error
	CheckSoftwareIntegrity(ctx context.Context) error
}

type KubernetesSetupManager interface {
	InstallCRIO(ctx context.Context) error
	InstallKubeadm(ctx context.Context) error
	InstallKubelet(ctx context.Context) error
	InstallKubectl(ctx context.Context) error
	InstallCilium(ctx context.Context) error
	InstallMetalLB(ctx context.Context) error
	InstallCNIPlugins(ctx context.Context) error
	ConfigureSysctl(ctx context.Context) error
	ConfigureContainerd(ctx context.Context) error
	ConfigureCNI(ctx context.Context) error
}
