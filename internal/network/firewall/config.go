// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"bytes"
	"os"

	"github.com/hashgraph/solo-weaver/pkg/sanity"
	"github.com/joomcode/errorx"
	"gopkg.in/yaml.v3"
)

// ConfigVersion is the schema version this build writes. A file may omit
// `version` (treated as the current schema) but may not declare a newer one:
// silently ignoring a field a future weaver understands could leave a host with
// a firewall narrower — or wider — than the file says.
const ConfigVersion = 1

// FileConfig is the declarative form of a Table: the schema of the
// `network firewall create --from-file` input, of `network firewall show
// --output yaml`, and of the persisted config the mutating verbs load. Those
// three being one type is what makes the round-trip exact — the output of
// `show --output yaml` re-applied via `--from-file` is a no-op by construction,
// not by coincidence.
//
// The reserved blocks are pointers so an absent section is distinguishable from
// an empty one. A file must state all three (see requireReservedBlocks): the
// file is the whole table, and a block left out would silently fall back to a
// compiled-in default — for `mgmt` that default is an empty allowlist under a
// default-drop policy, i.e. a lockout the operator never wrote down. A block
// present with an empty list renders no rule, which is how one is disabled.
type FileConfig struct {
	Version   int    `yaml:"version"`
	Mgmt      *Block `yaml:"mgmt,omitempty"`
	Blocked   *Block `yaml:"blocked,omitempty"`
	InCluster *Block `yaml:"in_cluster,omitempty"`
	Allow     []Rule `yaml:"allow,omitempty"`
}

// Block is a reserved section of the config file: the subset of Rule an operator
// may set on mgmt, blocked or in_cluster. It deliberately has no `name` (the
// section key is the name), no `proto` and no `icmp_echo` — the reserved blocks
// either fix those or have no use for them, and accepting the fields only to
// reject them in validation would suggest they mean something.
//
// Neither field carries `omitempty`: an empty list must survive a write as
// `cidrs: []`, because collapsing it to an absent key would turn "render no
// rule" back into "derive the default" on the next load.
type Block struct {
	CIDRs []string `yaml:"cidrs"`
	Ports []string `yaml:"ports"`
}

// LoadConfigFile reads and validates a declarative firewall config. Decoding is
// strict: an unrecognised key is an error rather than a silent no-op, since a
// typo in a firewall config would otherwise present as a rule that quietly
// never took effect.
func LoadConfigFile(path string) (*FileConfig, error) {
	clean, err := sanity.ValidateInputFile(path)
	if err != nil {
		return nil, errorx.IllegalArgument.Wrap(err, "invalid --from-file %q", path)
	}
	data, err := os.ReadFile(clean)
	if err != nil {
		return nil, errorx.ExternalError.Wrap(err, "failed to read --from-file %s", clean)
	}
	return ParseConfig(data)
}

// ParseConfig decodes and validates a declarative firewall config from YAML.
func ParseConfig(data []byte) (*FileConfig, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var c FileConfig
	if err := dec.Decode(&c); err != nil {
		return nil, errorx.IllegalFormat.Wrap(err, "failed to parse firewall config")
	}
	if c.Version != 0 && c.Version != ConfigVersion {
		return nil, errorx.IllegalFormat.New(
			"unsupported firewall config version %d: this build understands version %d", c.Version, ConfigVersion)
	}
	if err := c.requireReservedBlocks(); err != nil {
		return nil, err
	}

	// Validate through the model rather than field by field, so the config path
	// and the CLI path can never disagree about what is acceptable.
	t, err := c.Table()
	if err != nil {
		return nil, err
	}
	if err := t.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// requireReservedBlocks rejects a config that leaves a reserved block to a
// compiled-in default. The file states the whole table — nothing is carried over
// from the host's current firewall — so an omitted block is not "keep what is
// there", it is "take weaver's default". For `mgmt` that default is an empty
// address list under the input chain's default drop, which locks the operator
// out of new connections without the file ever saying so. Requiring all three to
// be written down makes the rendered posture readable from the file alone.
//
// `cidrs` is required inside `mgmt` and `blocked` for the same reason: a block
// header with only `ports` under it would re-open the same hole one level down.
// It stays optional inside `in_cluster`, where omitting it means "auto-detect
// this node's pod CIDR" — the one address list weaver can legitimately derive,
// and one whose absence costs a rule rather than access to the host.
//
// This runs on the persisted state file too, not only on --from-file.
// FileConfigFromTable always writes all three blocks, so a file weaver wrote
// passes by construction; one that fails has been truncated or hand-edited, and
// failing loudly beats loading it with a defaulted management allowlist.
func (c *FileConfig) requireReservedBlocks() error {
	for _, b := range []struct {
		name string
		// cidrsRequired is false only for in_cluster, whose omitted address list
		// means "auto-detect the pod CIDR" rather than "fall back to a default".
		cidrsRequired bool
		block         *Block
	}{
		{RuleMgmt, true, c.Mgmt},
		{RuleBlocked, true, c.Blocked},
		{RuleInCluster, false, c.InCluster},
	} {
		if b.block == nil {
			return errorx.IllegalFormat.New(
				"firewall config is missing the required %q block: the file states the whole table and reserved blocks are never "+
					"inherited from the host, so each one must be written down (use %q to render no rule for it)",
				b.name, b.name+": {cidrs: []}")
		}
		if b.cidrsRequired && b.block.CIDRs == nil {
			return errorx.IllegalFormat.New(
				"firewall config block %q is missing \"cidrs\": state the address list explicitly (use \"cidrs: []\" for none)", b.name)
		}
	}
	return nil
}

// Table builds the Table this config describes, applying the defaults for any
// omitted reserved field. The in-cluster address list is the one value it cannot
// resolve on its own — see InClusterCIDRsUnset.
func (c *FileConfig) Table() (*Table, error) {
	t := NewTable()

	if c.Mgmt != nil {
		t.Mgmt.CIDRs = c.Mgmt.CIDRs
		if c.Mgmt.Ports != nil {
			t.Mgmt.Ports = c.Mgmt.Ports
		}
	}
	if c.Blocked != nil {
		t.Blocked.CIDRs = c.Blocked.CIDRs
		// Carried across rather than dropped so Rule.Validate is the one place
		// that rejects a port on the block list — silently ignoring the field
		// would leave the operator believing they had narrowed the block.
		t.Blocked.Ports = c.Blocked.Ports
	}
	if c.InCluster != nil {
		t.InCluster.CIDRs = c.InCluster.CIDRs
		if c.InCluster.Ports != nil {
			t.InCluster.Ports = c.InCluster.Ports
		}
	}

	// UpsertAllow replaces a same-named rule, which is what the CLI wants but
	// would make a file listing one name twice silently keep only the last. In a
	// firewall config that is a typo worth failing on.
	seen := make(map[string]struct{}, len(c.Allow))
	for _, r := range c.Allow {
		if _, dup := seen[r.Name]; dup {
			return nil, errorx.IllegalFormat.New("duplicate allow rule %q", r.Name)
		}
		seen[r.Name] = struct{}{}
		if err := t.UpsertAllow(r); err != nil {
			return nil, err
		}
	}
	return t, nil
}

// InClusterCIDRsUnset reports whether the config left the in-cluster address
// list unspecified, so the caller knows to auto-detect the node's pod CIDR. An
// explicitly empty list (`in_cluster: {cidrs: []}`) is *specified* — it means
// "render no in-cluster rule" — and must not trigger detection.
//
// A parsed config always has the block itself (requireReservedBlocks), so in
// practice this reports on the `cidrs` field alone; the nil-block arm covers a
// config assembled in Go rather than decoded.
func (c *FileConfig) InClusterCIDRsUnset() bool {
	return c.InCluster == nil || c.InCluster.CIDRs == nil
}

// FileConfigFromTable is the inverse of Table: the declarative view of a table,
// with every reserved block written out explicitly so a subsequent load resolves
// to the same table without consulting a default or the cluster.
func FileConfigFromTable(t *Table) *FileConfig {
	return &FileConfig{
		Version:   ConfigVersion,
		Mgmt:      &Block{CIDRs: nonNil(t.Mgmt.CIDRs), Ports: nonNil(t.Mgmt.Ports)},
		Blocked:   &Block{CIDRs: nonNil(t.Blocked.CIDRs)},
		InCluster: &Block{CIDRs: nonNil(t.InCluster.CIDRs), Ports: nonNil(t.InCluster.Ports)},
		Allow:     t.Allow,
	}
}

// Marshal renders the config as YAML, for `show --output yaml` and for the
// persisted state file.
func (c *FileConfig) Marshal() ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(c); err != nil {
		return nil, errorx.InternalError.Wrap(err, "failed to render firewall config as YAML")
	}
	if err := enc.Close(); err != nil {
		return nil, errorx.InternalError.Wrap(err, "failed to render firewall config as YAML")
	}
	return buf.Bytes(), nil
}

// nonNil substitutes an empty slice for a nil one, so Block's non-omitempty
// fields marshal as `[]` rather than `null`. Both re-load as an explicitly empty
// list, but `[]` is what an operator would have written by hand.
func nonNil(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

// Blocked's port list is never populated: the block list drops every port, and
// Rule.Validate rejects a port on it. The field exists on Block only because one
// type serves all three reserved sections; writing it out for `blocked` would
// invite an operator to fill it in.
func (b *Block) MarshalYAML() (any, error) {
	if b.Ports == nil {
		return struct {
			CIDRs []string `yaml:"cidrs"`
		}{CIDRs: nonNil(b.CIDRs)}, nil
	}
	return struct {
		CIDRs []string `yaml:"cidrs"`
		Ports []string `yaml:"ports"`
	}{CIDRs: nonNil(b.CIDRs), Ports: nonNil(b.Ports)}, nil
}
