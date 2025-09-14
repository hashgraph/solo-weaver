package steps

import (
	"github.com/automa-saga/automa"
	"github.com/automa-saga/automa/automa_steps"
	"golang.hedera.com/solo-provisioner/internal/core"
	"path"
)

func SetupTempDirectoryStructure() automa.Builder {
	return automa_steps.NewMkdirStep("temp_directories", []string{
		path.Join(core.ProvisionerTempDir, "utils"),
		path.Join(core.ProvisionerTempDir, "cri-o", "unpack"),
		path.Join(core.ProvisionerTempDir, "kubernetes"),
		path.Join(core.ProvisionerTempDir, "cilium"),
	}, core.DefaultFilePerm)
}

func SetupSandboxDirectoryStructure() automa.Builder {
	return automa_steps.NewMkdirStep("sandbox_directories", []string{
		core.SandboxHomeDir,
		path.Join(core.SandboxHomeDir, "bin"),
		path.Join(core.SandboxHomeDir, "etc", "crio", "keys"),
		path.Join(core.SandboxHomeDir, "etc", "default"),
		path.Join(core.SandboxHomeDir, "etc", "sysconfig"),
		path.Join(core.SandboxHomeDir, "etc", "provisioner"),
		path.Join(core.SandboxHomeDir, "etc", "containers", "registries.conf.d"),
		path.Join(core.SandboxHomeDir, "etc", "cni", "net.d"),
		path.Join(core.SandboxHomeDir, "etc", "nri", "conf.d"),
		path.Join(core.SandboxHomeDir, "etc", "kubernetes", "pki"),
		path.Join(core.SandboxHomeDir, "var", "lib", "etcd"),
		path.Join(core.SandboxHomeDir, "var", "lib", "containers", "storage"),
		path.Join(core.SandboxHomeDir, "var", "lib", "kubelet"),
		path.Join(core.SandboxHomeDir, "var", "lib", "crio"),
		path.Join(core.SandboxHomeDir, "var", "run", "cilium"),
		path.Join(core.SandboxHomeDir, "var", "run", "nri"),
		path.Join(core.SandboxHomeDir, "var", "run", "containers", "storage"),
		path.Join(core.SandboxHomeDir, "var", "run", "crio", "exits"),
		path.Join(core.SandboxHomeDir, "var", "logs", "crio", "pods"),
		path.Join(core.SandboxHomeDir, "run", "runc"),
		path.Join(core.SandboxHomeDir, "usr", "libexec", "crio"),
		path.Join(core.SandboxHomeDir, "usr", "lib", "systemd", "system", "kubelet.service.d"),
		path.Join(core.SandboxHomeDir, "usr", "local", "bin"),
		path.Join(core.SandboxHomeDir, "usr", "local", "share", "man"),
		path.Join(core.SandboxHomeDir, "usr", "local", "share", "oci-umount", "oci-umount.d"),
		path.Join(core.SandboxHomeDir, "usr", "local", "share", "bash-completion", "completions"),
		path.Join(core.SandboxHomeDir, "usr", "local", "share", "fish", "completions"),
		path.Join(core.SandboxHomeDir, "usr", "local", "share", "zsh", "site-functions"),
		path.Join(core.SandboxHomeDir, "opt", "cni", "bin"),
		path.Join(core.SandboxHomeDir, "opt", "nri", "plugins"),
	}, core.DefaultFilePerm)
}

func SetupBindMountsDirectoryStructure() automa.Builder {
	return automa_steps.NewMkdirStep("bind_mounts_directories", []string{
		"/etc/kubernetes",
		"/var/lib/kubelet",
	}, core.DefaultFilePerm)
}

func SetupProvisionerHomeDirectoryStructure() automa.Builder {
	return automa_steps.NewMkdirStep("home_directories", []string{
		path.Join(core.ProvisionerHomeDir, "bin"),
		path.Join(core.ProvisionerHomeDir, "logs"),
		path.Join(core.ProvisionerHomeDir, "config"),
	}, core.DefaultFilePerm)
}

func SetupDirectories() automa.Builder {
	return automa.NewWorkFlowBuilder("setup_directories").
		Steps(
			SetupTempDirectoryStructure(),
			SetupSandboxDirectoryStructure(),
			SetupSandboxDirectoryStructure(),
			SetupBindMountsDirectoryStructure(),
			SetupProvisionerHomeDirectoryStructure(),
		)
}
