package software

type kubeletInstaller struct {
}

func (ki *kubeletInstaller) Download() error {
	// Downloads and check the integrity of the downloaded package

	return nil
}

func (ki *kubeletInstaller) Extract() error {
	return nil
}

func (ki *kubeletInstaller) Install() error {

	// mv to sandbox

	// installation/binary symlink

	return nil
}

// Verify performs binary integrity check
func (ki *kubeletInstaller) Verify() error {
	return nil
}

// Checks the directories and highlevel contents in sandbox
// and checks integrity/existence of binary symlink
func (ki *kubeletInstaller) IsInstalled() (bool, error) {
	return false, nil
}

func (ki *kubeletInstaller) Configure() error {
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
func (ki *kubeletInstaller) IsConfigured() (bool, error) {
	return false, nil
}
