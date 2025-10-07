package software

type k9sInstaller struct {
}

func (ki *k9sInstaller) Download() error {
	// Downloads and check the integrity of the downloaded package

	return nil
}

func (ki *k9sInstaller) Extract() error {
	return nil
}

func (ki *k9sInstaller) Install() error {

	// mv to sandbox

	// installation/binary symlink

	return nil
}

// Verify performs binary integrity check
func (ki *k9sInstaller) Verify() error {
	return nil
}

// Checks the directories and highlevel contents in sandbox
// and checks integrity/existence of binary symlink
func (ki *k9sInstaller) IsInstalled() (bool, error) {
	return false, nil
}

func (ki *k9sInstaller) Configure() error {
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
func (ki *k9sInstaller) IsConfigured() (bool, error) {
	return false, nil
}
