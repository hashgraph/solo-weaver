package software

type helmInstaller struct {
}

func (hi *helmInstaller) Download() error {
	// Downloads and check the integrity of the downloaded package

	return nil
}

func (hi *helmInstaller) Extract() error {
	return nil
}

func (hi *helmInstaller) Install() error {

	// mv to sandbox

	// installation/binary symlink

	return nil
}

// Verify performs binary integrity check
func (hi *helmInstaller) Verify() error {
	return nil
}

// Checks the directories and highlevel contents in sandbox
// and checks integrity/existence of binary symlink
func (hi *helmInstaller) IsInstalled() (bool, error) {
	return false, nil
}

func (hi *helmInstaller) Configure() error {
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
func (hi *helmInstaller) IsConfigured() (bool, error) {
	return false, nil
}
