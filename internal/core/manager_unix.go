package core

import "context"

type unixSetupManager struct{}

func (w *unixSetupManager) SetupDirectories(ctx context.Context) error       { return nil }
func (w *unixSetupManager) UpdateOS(ctx context.Context) error               { return nil }
func (w *unixSetupManager) UpgradeOS(ctx context.Context) error              { return nil }
func (w *unixSetupManager) DisableSwap(ctx context.Context) error            { return nil }
func (w *unixSetupManager) InstallIpTables(ctx context.Context) error        { return nil }
func (w *unixSetupManager) InstallGPG(ctx context.Context) error             { return nil }
func (w *unixSetupManager) InstallCurl(ctx context.Context) error            { return nil }
func (w *unixSetupManager) InstallConntrack(ctx context.Context) error       { return nil }
func (w *unixSetupManager) InstallEBTables(ctx context.Context) error        { return nil }
func (w *unixSetupManager) InstallSoCat(ctx context.Context) error           { return nil }
func (w *unixSetupManager) InstallNFTables(ctx context.Context) error        { return nil }
func (w *unixSetupManager) InstallKernelModules(ctx context.Context) error   { return nil }
func (w *unixSetupManager) RemoveContainerd(ctx context.Context) error       { return nil }
func (w *unixSetupManager) RemoveUnusedPackages(ctx context.Context) error   { return nil }
func (w *unixSetupManager) CheckHardware(ctx context.Context) error          { return nil }
func (w *unixSetupManager) CheckSoftwareIntegrity(ctx context.Context) error { return nil }

func NewUnixSetupManager() *unixSetupManager {
	return &unixSetupManager{}
}
