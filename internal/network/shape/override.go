// SPDX-License-Identifier: Apache-2.0

package shape

import "github.com/joomcode/errorx"

// ClassOverride carries operator-supplied overrides for one HTB class's
// bandwidth fields, parsed from
// `block node install --shape <class>=rate=...,ceil=...,prio=...`. A zero-value
// field (empty Rate/Ceil, nil Prio) means "keep the profile default", so an
// operator can override just one field (e.g. only --shape publisher=ceil=1gbit)
// without restating the others.
type ClassOverride struct {
	Rate string
	Ceil string
	Prio *int
}

// ValidateClassOverride checks that name is a known class and that each set
// field is individually valid (rate/ceil parseable, ceil >= rate when both are
// set, prio in [0,7]). It deliberately does NOT check the sum-of-rates
// constraint — that is enforced against the full merged class set when the shape
// is provisioned (see validateProvisionedClasses), because the sum depends on
// the profile defaults the override merges into.
func ValidateClassOverride(name string, o ClassOverride) error {
	if _, err := lookupClassInfo(name); err != nil {
		return err
	}
	if o.Rate != "" {
		if err := validateRate(o.Rate); err != nil {
			return errorx.Decorate(err, "invalid --shape rate for class %q", name)
		}
	}
	if o.Ceil != "" {
		if err := validateRate(o.Ceil); err != nil {
			return errorx.Decorate(err, "invalid --shape ceil for class %q", name)
		}
	}
	if o.Rate != "" && o.Ceil != "" {
		if err := validateCeilGeRate(o.Ceil, o.Rate); err != nil {
			return err
		}
	}
	if o.Prio != nil {
		if err := validatePrio(*o.Prio); err != nil {
			return err
		}
	}
	return nil
}

// ClassDirection returns the tc device direction ("ingress" or "egress") a
// class belongs to, so callers can route a --shape override to the device it
// applies to. Unknown class names return an error naming the known classes.
func ClassDirection(name string) (string, error) {
	info, err := lookupClassInfo(name)
	if err != nil {
		return "", err
	}
	return info.Dir, nil
}

// applyClassOverrides merges overrides into the matching classes by name,
// leaving unmatched classes and unset override fields at their profile
// defaults. Overrides naming a class not in classes (e.g. an egress class when
// provisioning the ingress device) are ignored here — each provision call sees
// the full override map and applies only the entries for its own direction.
func applyClassOverrides(classes []*ClassConfig, overrides map[string]ClassOverride) {
	for _, c := range classes {
		o, ok := overrides[c.Name]
		if !ok {
			continue
		}
		if o.Rate != "" {
			c.Rate = o.Rate
		}
		if o.Ceil != "" {
			c.Ceil = o.Ceil
		}
		if o.Prio != nil {
			c.Prio = *o.Prio
		}
	}
}

// validateProvisionedClasses re-checks the merged class set (profile defaults
// with any --shape overrides applied) before it is written: each class's
// rate/ceil/prio must be valid, and the sum of class rates must not exceed the
// device root rate. The profile defaults always pass; this exists to catch an
// override that individually validates but pushes the total over the trunk
// budget.
func validateProvisionedClasses(classes []*ClassConfig, deviceRate string) error {
	for _, c := range classes {
		if err := validateRate(c.Rate); err != nil {
			return errorx.Decorate(err, "invalid rate for class %q", c.Name)
		}
		if c.Ceil != "" {
			if err := validateCeilGeRate(c.Ceil, c.Rate); err != nil {
				return errorx.Decorate(err, "class %q", c.Name)
			}
		}
		if err := validatePrio(c.Prio); err != nil {
			return errorx.Decorate(err, "class %q", c.Name)
		}
	}
	deviceBps, err := parseBandwidthBps(deviceRate)
	if err != nil {
		return nil // device rate unparseable (legacy expression): skip the sum check
	}
	var sum int64
	for _, c := range classes {
		bps, err := parseBandwidthBps(c.Rate)
		if err != nil {
			continue
		}
		sum += bps
	}
	if sum > deviceBps {
		return errorx.IllegalArgument.New(
			"total class rates (%d bit) exceed the device root rate %s (%d bit) after --shape overrides; lower a --shape rate or raise --link-rate",
			sum, deviceRate, deviceBps)
	}
	return nil
}
