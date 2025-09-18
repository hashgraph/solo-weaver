package systemd

import "golang.hedera.com/solo-provisioner/internal/templates"

type Client struct {
}

func test() {
	templates.Files.ReadFile("sysctl/network-performance.conf")
}
