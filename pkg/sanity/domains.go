// SPDX-License-Identifier: Apache-2.0

package sanity

// AllowedDomains is an allowlist of trusted domains for software downloads.
// This list is used by ValidateURL to prevent downloads from untrusted sources
// and protect against SSRF (Server-Side Request Forgery) attacks.
//
// SECURITY GUIDELINES - WHAT TO ADD:
// Only HTTPS URLs from these domains (and their subdomains) will be allowed.
// When adding new domains, ensure they are:
//  1. Trusted and reputable sources (e.g., official software registries)
//  2. Use HTTPS with valid certificates
//  3. Have a legitimate business need for software downloads
//  4. Documented with a comment explaining their purpose
//
// SECURITY GUIDELINES - WHAT NOT TO ADD:
// DO NOT add any of the following, as they pose security risks:
//   - Cloud metadata IP addresses:
//   - 169.254.169.254 (AWS, Azure, OpenStack metadata)
//   - fd00:ec2::254 (AWS IMDSv2 IPv6)
//   - 169.254.169.123 (DigitalOcean metadata)
//   - 169.254.169.250 (Oracle Cloud metadata)
//   - metadata.google.internal (GCP metadata)
//   - Loopback addresses:
//   - 127.0.0.0/8 (IPv4 loopback range)
//   - ::1 (IPv6 loopback)
//   - localhost
//   - Private IP ranges (RFC 1918):
//   - 10.0.0.0/8
//   - 172.16.0.0/12
//   - 192.168.0.0/16
//   - Link-local addresses:
//   - 169.254.0.0/16 (IPv4 link-local)
//   - fe80::/10 (IPv6 link-local)
//   - Unspecified addresses:
//   - 0.0.0.0 (IPv4)
//   - :: (IPv6)
//   - Any IP addresses instead of domain names
//   - Internal or development domains (e.g., .local, .internal, .test)
//   - Domains that redirect to untrusted sources
//
// The domain allowlist is the primary security control. Adding inappropriate
// domains can bypass SSRF protections and expose the system to attacks.
var allowedDomains = []string{
	// Google Cloud Storage - used for various Kubernetes components
	"storage.googleapis.com",
	"dl.google.com",

	// GitHub releases and content
	"github.com",
	"githubusercontent.com",

	// Kubernetes official
	"dl.k8s.io",
	"packages.cloud.google.com",

	// Container registries
	"gcr.io",
	"registry.k8s.io",
	"quay.io",

	// Helm charts and releases
	"charts.helm.sh",
	"get.helm.sh",

	// HashiCorp releases
	"releases.hashicorp.com",

	// Teleport - secure access platform
	"hashgraph.teleport.sh",
}

// AllowedDomains returns the allowlist of trusted domains for software downloads.
func AllowedDomains() []string {
	return append([]string(nil), allowedDomains...)
}
