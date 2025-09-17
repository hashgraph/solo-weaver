package core

import "context"

type unixKubernetesSetupManager struct{}

func (w *unixKubernetesSetupManager) InstallCRIO(ctx context.Context) error         { return nil }
func (w *unixKubernetesSetupManager) InstallKubeadm(ctx context.Context) error      { return nil }
func (w *unixKubernetesSetupManager) InstallKubelet(ctx context.Context) error      { return nil }
func (w *unixKubernetesSetupManager) InstallKubectl(ctx context.Context) error      { return nil }
func (w *unixKubernetesSetupManager) InstallCilium(ctx context.Context) error       { return nil }
func (w *unixKubernetesSetupManager) InstallMetalLB(ctx context.Context) error      { return nil }
func (w *unixKubernetesSetupManager) InstallCNIPlugins(ctx context.Context) error   { return nil }
func (w *unixKubernetesSetupManager) ConfigureSysctl(ctx context.Context) error     { return nil }
func (w *unixKubernetesSetupManager) ConfigureContainerd(ctx context.Context) error { return nil }
func (w *unixKubernetesSetupManager) ConfigureCNI(ctx context.Context) error        { return nil }

func NewUnixKubernetesSetupManager() KubernetesSetupManager {
	return &unixKubernetesSetupManager{}
}
