package software

type kubeadmInstaller struct {
}

func (ki *kubeadmInstaller) Download() error {
	// Downloads and check the integrity of the downloaded package

	return nil
}

func (ki *kubeadmInstaller) Extract() error {
	return nil
}

func (ki *kubeadmInstaller) Install() error {

	// mv to sandbox

	// installation/binary symlink

	return nil
}

// Verify performs binary integrity check
func (ki *kubeadmInstaller) Verify() error {
	return nil
}

// Checks the directories and highlevel contents in sandbox
// and checks integrity/existence of binary symlink
func (ki *kubeadmInstaller) IsInstalled() (bool, error) {
	return false, nil
}

func (ki *kubeadmInstaller) Configure() error {
	// default configuration
	//	/etc/default/crio

	// service configuration
	//	/usr/lib/systemd/system/crio.service

	// application configuration
	// 	/etc/crio/crio.conf.d

	// configuration service symlink
	// 	/usr/lib/systemd/system/crio.service

	return nil
}

// Checks default, service, application and configuration service symlinks
func (ki *kubeadmInstaller) IsConfigured() (bool, error) {
	return false, nil
}
