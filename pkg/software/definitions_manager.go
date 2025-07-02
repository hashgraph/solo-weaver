package software

import (
	"encoding/json"
	"github.com/cockroachdb/errors"
	"golang.hedera.com/solo-provisioner/pkg/software/specs"
)

type definitionsManager struct {
	specs map[specs.SoftwareName]specs.SoftwareDefinition
}

func (dm *definitionsManager) HasDefinition(name specs.SoftwareName) bool {
	if _, found := dm.specs[name]; found {
		return true
	}

	return false
}

func (dm *definitionsManager) GetDefinition(name specs.SoftwareName) (specs.SoftwareDefinition, error) {
	s, found := dm.specs[name]
	if !found {
		return specs.SoftwareDefinition{}, errors.Newf("unable to find definition for %q", name)
	}

	return s, nil
}

// parseDefinitionJSON parse the json software definition
func (dm *definitionsManager) parseDefinitionJSON(specJSON string) (specs.SoftwareDefinition, error) {
	definition := specs.SoftwareDefinition{}
	err := json.Unmarshal([]byte(specJSON), &definition)
	return definition, err
}

func (dm *definitionsManager) GetAll() []specs.SoftwareDefinition {
	var items []specs.SoftwareDefinition
	for _, def := range dm.specs {
		items = append(items, def)
	}
	return items
}

// loadAll loads all the software definition and stores in the manager
func (dm *definitionsManager) loadAll() error {
	if dm.specs == nil {
		dm.specs = map[specs.SoftwareName]specs.SoftwareDefinition{}
	}

	for key, jsonString := range definitionMapping {
		s, err := dm.parseDefinitionJSON(jsonString)
		if err != nil {
			return errors.Wrapf(err, "failed to parse software definition for %q", key)
		}

		dm.specs[key] = s
	}

	return nil
}

func NewDefinitionManager() (DefinitionManager, error) {
	dm := &definitionsManager{}
	err := dm.loadAll()
	if err != nil {
		return nil, err
	}

	return dm, nil
}
