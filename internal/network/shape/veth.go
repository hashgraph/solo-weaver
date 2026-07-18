// SPDX-License-Identifier: Apache-2.0

package shape

import (
	"context"

	"github.com/automa-saga/logx"
	"github.com/joomcode/errorx"
)

// validateVethName rejects a veth name that could inject into the privileged tc
// invocation. The name originates from the daemon's runtime veth resolution but
// is an untrusted argument at the command boundary, so it passes through the
// same IFNAMSIZ-bounded funnel (nicNameRe) as the egress NIC name.
func validateVethName(veth string) error {
	if !nicNameRe.MatchString(veth) {
		return errorx.IllegalArgument.New(
			"veth name %q is invalid: must match %s", veth, nicNameRe.String())
	}
	return nil
}

// effectiveIngressRootRate resolves the concrete HTB trunk rate for the ingress
// veth hierarchy. By design the ingress root rate mirrors the egress root rate
// (the install's `--ingress-bandwidth` defaults to `--egress-bandwidth`), so a
// recorded ingress rate that is not a concrete bandwidth — e.g. left as "auto",
// which has no meaning for a per-pod veth that has no sysfs link speed — falls
// back to the recorded egress rate. It errors only when neither is concrete.
func effectiveIngressRootRate(ingressRate, egressRate string) (string, error) {
	if validateRate(ingressRate) == nil {
		return ingressRate, nil
	}
	if validateRate(egressRate) == nil {
		return egressRate, nil
	}
	return "", errorx.IllegalState.New(
		"ingress shape rate %q is not a concrete bandwidth and no egress rate is available to mirror; "+
			"run `network shape create --device ingress --rate <bandwidth>` (or configure the egress device)", ingressRate)
}

// ApplyIngressVeth installs the $VETH ingress HTB hierarchy (design §5.1) on the
// given host-side veth, using the ingress device root and per-class budgets
// recorded by `network shape` (under DeviceConfigDir / ClassConfigDir). It is
// the privileged operation the daemon's pod-lifecycle watcher delegates via
// `block node tc-attach` on each BN pod create.
//
// The hierarchy is: root HTB qdisc (default → the ingress default class), a
// trunk class 1:1 at the device root rate, one leaf class per recorded ingress
// class (1:10 / 1:20 / 1:30) with an fq_codel qdisc, and no tc filters — HTB
// classifies natively on skb->priority set by the nft classification rules.
//
// The apply is idempotent: the root qdisc is torn down first (cascading to all
// classes and leaf qdiscs), so a rebind on a recycled veth name starts clean.
func (m *Manager) ApplyIngressVeth(ctx context.Context, veth string) error {
	if err := validateVethName(veth); err != nil {
		return err
	}

	dev, err := readDevice(DirIngress)
	if err != nil {
		return err
	}
	if dev == nil {
		return errorx.IllegalState.New(
			"no ingress shape recorded; run `network shape create --device ingress --rate <bandwidth> --default reserve-ingress` first (normally done by `block node install`)")
	}
	defInfo, err := lookupClassInfo(dev.DefaultClass)
	if err != nil {
		return err
	}

	egress, err := readDevice(DirEgress)
	if err != nil {
		return err
	}
	egressRate := ""
	if egress != nil {
		egressRate = egress.Rate
	}
	rootRate, err := effectiveIngressRootRate(dev.Rate, egressRate)
	if err != nil {
		return err
	}

	classes, err := loadClassesForDir(DirIngress)
	if err != nil {
		return err
	}
	if len(classes) == 0 {
		return errorx.IllegalState.New(
			"no ingress shape classes recorded; run `network shape create --class <name> --rate <bandwidth>` for the ingress classes first (normally done by `block node install`)")
	}

	if err := m.applyVethHierarchy(ctx, veth, defInfo.Minor, rootRate, classes); err != nil {
		return err
	}

	logx.As().Info().
		Str("veth", veth).
		Str("default_class", dev.DefaultClass).
		Int("classes", len(classes)).
		Msg("installed $VETH ingress HTB hierarchy")
	return nil
}

// applyVethHierarchy installs the §5.1 HTB hierarchy on veth under the tc lock:
// tear down any existing root qdisc, add the root HTB qdisc (default →
// defaultMinor), the trunk class 1:1 at rootRate, then one leaf class + fq_codel
// per recorded class. Split from ApplyIngressVeth (which does the disk reads and
// validation) so the tc-call sequence is unit-testable with a fake TCRunner.
func (m *Manager) applyVethHierarchy(ctx context.Context, veth, defaultMinor, rootRate string, classes []*ClassConfig) error {
	return m.withLock(func() error {
		// Tear down any existing hierarchy so a rebind on a recycled veth name
		// (or a partially-applied prior attempt) starts from a clean slate.
		if err := m.tcRunner.QdiscDelRoot(ctx, veth); err != nil {
			return err
		}
		if err := m.tcRunner.QdiscAddRoot(ctx, veth, defaultMinor); err != nil {
			return err
		}
		if err := m.tcRunner.ClassAddRoot(ctx, veth, rootRate, rootRate); err != nil {
			return err
		}
		for _, cls := range classes {
			ci, err := lookupClassInfo(cls.Name)
			if err != nil {
				return err
			}
			if err := m.tcRunner.ClassAdd(ctx, veth, ci.Minor, cls.Rate, cls.effectiveCeil(), cls.Prio); err != nil {
				return err
			}
			if err := m.tcRunner.QdiscAddFqCodel(ctx, veth, ci.Minor, ci.Handle); err != nil {
				return err
			}
		}
		return nil
	})
}

// RemoveIngressVeth tears down the $VETH ingress HTB hierarchy on the given
// veth. It is best-effort: the kernel auto-removes veth-attached qdiscs when the
// veth disappears on pod delete, so this is mostly for the proactive
// (re)attach-time cleanup path and never fails on an already-absent qdisc.
func (m *Manager) RemoveIngressVeth(ctx context.Context, veth string) error {
	if err := validateVethName(veth); err != nil {
		return err
	}
	return m.withLock(func() error {
		return m.tcRunner.QdiscDelRoot(ctx, veth)
	})
}
