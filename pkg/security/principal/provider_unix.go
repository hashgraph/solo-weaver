//go:build !darwin && !windows

package principal

const (
	unixPasswordFile = "/etc/passwd"
	unixGroupFile    = "/etc/group"
)

type unixProvider struct{}

func NewProvider() Provider {
	return &unixProvider{}
}

func (p *unixProvider) EnumerateUsers(m Manager) ([]User, error) {
	entities, err := readEntityFile(unixPasswordFile, parseUnixUser)
	if err != nil {
		return nil, err
	}

	users := make([]User, len(entities))
	for i, e := range entities {
		e.manager = m
		users[i] = e
	}

	return users, nil
}

func (p *unixProvider) EnumerateGroups(m Manager) ([]Group, error) {
	entities, err := readEntityFile(unixGroupFile, parseUnixGroup)
	if err != nil {
		return nil, err
	}

	groups := make([]Group, len(entities))
	for i, e := range entities {
		e.manager = m
		groups[i] = e
	}

	return groups, nil
}
