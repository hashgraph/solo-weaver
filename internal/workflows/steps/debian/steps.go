package debian

import (
	"github.com/automa-saga/automa"
	"github.com/automa-saga/automa/automa_steps"
	"golang.hedera.com/solo-provisioner/internal/core"
)

func UpdateDebianOS() automa.Builder {
	return automa_steps.NewBashScriptStep("update_debian_os", []string{
		"apt-get update"}, core.ProvisionerTempDir)
}

func UpgradeDebianOS() automa.Builder {
	return automa_steps.NewBashScriptStep("upgrade_debian_os", []string{
		"apt-get upgrade -y"}, core.ProvisionerTempDir)
}

func RemoveUnusedPackages() automa.Builder {
	return automa_steps.NewBashScriptStep("auto_remove_debian_os", []string{
		"apt-get autoremove -y"}, core.ProvisionerTempDir)
}

func DisableSwap() automa.Builder {
	return automa_steps.NewBashScriptStep("disable_swap", []string{
		`sed -i.bak 's/^\(.*\sswap\s.*\)$/#\1\n/' /etc/fstab`,
		"swapoff -a",
	}, core.ProvisionerTempDir)
}

func RemoveExistingContainerd() automa.Builder {
	return automa_steps.NewBashScriptStep("remove_existing_containerd", []string{
		"apt-get remove -y containerd containerd.io || true",
	}, core.ProvisionerTempDir)
}

func InstallIpTables() automa.Builder {
	return automa_steps.NewBashScriptStep("install_iptables", []string{
		"apt-get install -y iptables",
	}, core.ProvisionerTempDir)
}

func InstallGPG() automa.Builder {
	return automa_steps.NewBashScriptStep("install_gpg", []string{
		"apt-get install -y gnupg2",
	}, core.ProvisionerTempDir)
}

func InstallCurl() automa.Builder {
	return automa_steps.NewBashScriptStep("install_curl", []string{
		"apt-get install -y curl",
	}, core.ProvisionerTempDir)
}

func InstallConntrack() automa.Builder {
	return automa_steps.NewBashScriptStep("install_conntrack", []string{
		"apt-get install -y conntrack",
	}, core.ProvisionerTempDir)
}

func InstallEBTables() automa.Builder {
	return automa_steps.NewBashScriptStep("install_ebtables", []string{
		"apt-get install -y ebtables",
	}, core.ProvisionerTempDir)
}

func InstallSoCat() automa.Builder {
	return automa_steps.NewBashScriptStep("install_socat", []string{
		"apt-get install -y socat",
	}, core.ProvisionerTempDir)
}

func InstallNFTables() automa.Builder {
	return automa_steps.NewBashScriptStep("install_nftables", []string{
		"apt-get install -y nftables",
	}, core.ProvisionerTempDir)
}

func EnableNFTables() automa.Builder {
	return automa_steps.NewBashScriptStep("enable_nftables", []string{
		"systemctl enable nftables",
		"systemctl start nftables",
	}, core.ProvisionerTempDir)
}

func InstallKernelModules() automa.Builder {
	return automa_steps.NewBashScriptStep("install_kernel_modules", []string{
		"modprobe overlay",
		"modprobe br_netfilter",
		`echo "overlay" > /etc/modules-load.d/overlay.conf`,
		`echo "br_netfilter" > /etc/modules-load.d/br_netfilter.conf`,
	}, core.ProvisionerTempDir)
}
