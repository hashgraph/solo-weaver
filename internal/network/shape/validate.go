// SPDX-License-Identifier: Apache-2.0

package shape

import (
	"strconv"
	"strings"

	"github.com/joomcode/errorx"
)

// validateDir checks that dir is a valid tc device direction.
func validateDir(dir string) error {
	if dir != DirIngress && dir != DirEgress {
		return errorx.IllegalArgument.New("--device must be %q or %q, got %q", DirIngress, DirEgress, dir)
	}
	return nil
}

// validatePrio checks that prio is in the HTB priority range [0, 7].
func validatePrio(prio int) error {
	if prio < 0 || prio > 7 {
		return errorx.IllegalArgument.New("--prio must be in [0,7], got %d", prio)
	}
	return nil
}

// validateDefaultClass checks that className is a valid §5 class for dir.
func validateDefaultClass(className, dir string) error {
	info, err := lookupClassInfo(className)
	if err != nil {
		return err
	}
	if info.Dir != dir {
		return errorx.IllegalArgument.New(
			"class %q is a %s class and cannot be the default for the %s device; valid defaults: %s",
			className, info.Dir, dir, strings.Join(knownClassNamesForDir(dir), ", "))
	}
	return nil
}

// parseBandwidthBps parses a tc-style bandwidth string and returns its value
// in bits per second. Supports suffixes: bit, kbit, mbit, gbit (case-insensitive).
// Returns an error for unknown suffixes or non-numeric values. Shell arithmetic
// expressions (e.g. "$(( SPEED * 40 / 100 ))mbit") are intentionally not
// supported — they are only used in the legacy default scriptData path.
func parseBandwidthBps(s string) (int64, error) {
	low := strings.ToLower(strings.TrimSpace(s))
	var multiplier int64
	var numStr string
	switch {
	case strings.HasSuffix(low, "gbit"):
		multiplier = 1_000_000_000
		numStr = low[:len(low)-4]
	case strings.HasSuffix(low, "mbit"):
		multiplier = 1_000_000
		numStr = low[:len(low)-4]
	case strings.HasSuffix(low, "kbit"):
		multiplier = 1_000
		numStr = low[:len(low)-4]
	case strings.HasSuffix(low, "bit"):
		multiplier = 1
		numStr = low[:len(low)-3]
	default:
		return 0, errorx.IllegalArgument.New(
			"invalid bandwidth %q: expected a number followed by bit/kbit/mbit/gbit (e.g. 100mbit)", s)
	}
	n, err := strconv.ParseFloat(numStr, 64)
	if err != nil || n < 0 {
		return 0, errorx.IllegalArgument.New("invalid bandwidth %q: numeric part must be a non-negative number", s)
	}
	return int64(n * float64(multiplier)), nil
}

// validateRate checks that rate is a non-empty, parseable tc bandwidth string.
func validateRate(rate string) error {
	if strings.TrimSpace(rate) == "" {
		return errorx.IllegalArgument.New("--rate must not be empty")
	}
	_, err := parseBandwidthBps(rate)
	return err
}

// validateCeilGeRate checks that ceil >= rate when both are parseable. Silently
// skips the check when either value cannot be parsed (tc will validate at apply
// time; legacy shell expressions used in default renders are never user-supplied).
func validateCeilGeRate(ceil, rate string) error {
	ceilBps, err1 := parseBandwidthBps(ceil)
	rateBps, err2 := parseBandwidthBps(rate)
	if err1 != nil || err2 != nil {
		return nil
	}
	if ceilBps < rateBps {
		return errorx.IllegalArgument.New("--ceil %s must be >= --rate %s", ceil, rate)
	}
	return nil
}

// validateSumRates checks that the total of all class rates for the same device
// (including cfg, replacing any existing entry with the same name) does not
// exceed the device root rate. Skips the check if any value cannot be parsed.
func validateSumRates(existing []*ClassConfig, cfg *ClassConfig, deviceRate string) error {
	deviceBps, err := parseBandwidthBps(deviceRate)
	if err != nil {
		return nil // device rate unparseable (legacy expression): skip
	}
	var sum int64
	for _, c := range existing {
		if c.Name == cfg.Name {
			continue // will be replaced by cfg in the sum
		}
		bps, err := parseBandwidthBps(c.Rate)
		if err != nil {
			continue // unparseable sibling: skip its contribution
		}
		sum += bps
	}
	newBps, err := parseBandwidthBps(cfg.Rate)
	if err != nil {
		return nil // new rate unparseable: skip
	}
	sum += newBps
	if sum > deviceBps {
		return errorx.IllegalArgument.New(
			"total class rates (%d bit) would exceed device root rate %s (%d bit); reduce --rate or raise the device rate",
			sum, deviceRate, deviceBps)
	}
	return nil
}
