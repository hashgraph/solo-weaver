package software

type crioInstaller struct {
}

func (ci *crioInstaller) Download() error {
	// Downloads and check the integrity of the downloaded package

	return nil
}

func (ci *crioInstaller) Extract() error {
	return nil
}

func (ci *crioInstaller) Install() error {

	// mv to sandbox

	// installation/binary symlink

	return nil
}

// Verify performs binary integrity check
func (ci *crioInstaller) Verify() error {
	return nil
}

// Checks the directories and highlevel contents in sandbox
// and checks integrity/existence of binary symlink
func (ci *crioInstaller) IsInstalled() (bool, error) {
	return false, nil
}

func (ci *crioInstaller) Configure() error {
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
func (ci *crioInstaller) IsConfigured() (bool, error) {
	return false, nil
}
