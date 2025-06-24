/*
 * Copyright 2016-2022 Hedera Hashgraph, LLC
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package version

import (
	"bytes"
	"fmt"
	"github.com/cockroachdb/errors"
	"regexp"
	"strconv"
	"strings"
)

// Version represents a Semantic Version.
// It also exposes various version comparison functionalities.
//
// It uses Semantic Versioning specification to handle the version comparison logic.
//
// If a version is not SemVer formatted, it converts that into a SemVer format:
//   - "219" will be converted into 219.0.0 internally.
//   - "v1.2" will be converted to "1.2.0" internally.
//   - "8.30" will be converted to "8.30.0" internally.
//
// preRelease part of the version is compared lexicographically for simplicity of implementation. The reason for such
// trade-off is we only expect proper released version of the software to be installed on the nodes. However, if there is
// a preRelease-release part in the version, it will be considered lower precedence if major, minor, and patch are equal,
// which adheres to spec https://semver.org/#spec-item-11. If both versions have preRelease-release part, preRelease
// parts are compared lexicographically.
type Version struct {
	// raw is the source version string before parsing
	raw string

	// versionStr denotes the string representation of Version after parsing
	versionStr string

	// major represents the major part of the Version.
	major uint64

	// minor represents the minor part of the Version.
	minor uint64

	// patch represents the patch part of the Version.
	patch uint64

	// preRelease represents pre-release part of the Version separated by hyphen
	//
	// https://semver.org/#spec-item-9
	// Examples: 1.0.0-alpha, 1.0.0-beta.1, 1.0.0-rc.1, 1.0.0-rc.2+abd7, 1.0.0-0.3.7, 1.0.0-x.7.z.92, 1.0.0-x-y-z.--.
	// preRelease release is compared lexicographically for simplicity.
	preRelease string

	// build represents the build section of the Version separated by plus sign.
	// It could be the commit sha, label or any other metadata.
	// Note: during LessThan or GreaterThan comparisons, build part is ignored since lexicographical string comparison may
	// not be valid.
	build string
}

// LessThan checks if it is less than the input version v2
// Ref: https://semver.org/#spec-item-11
func (v *Version) LessThan(v2 Version) bool {
	if v.major < v2.major {
		return true
	} else if v.major > v2.major {
		return false
	}

	if v.minor < v2.minor {
		return true
	} else if v.minor > v2.minor {
		return false
	}

	if v.patch < v2.patch {
		return true
	} else if v.patch > v2.patch {
		return false
	}

	if v.preRelease != "" && v2.preRelease == "" {
		return true
	} else if v.preRelease == "" && v2.preRelease != "" {
		return false
	}

	return v.isPreLessThan(v2.preRelease)
}

// EqualTo checks if it is equal to the input version
// It performs a string comparison of the raw in full.
func (v *Version) EqualTo(v2 Version) bool {
	return v.raw == v2.raw
}

// LessOrEqual return true if it is less than or equal to the input version v2
// This is just a wrapper function around LessThan and EqualTo
func (v *Version) LessOrEqual(v2 Version) bool {
	return v.LessThan(v2) || v.EqualTo(v2)
}

// GreaterOrEqual return true if it is greater than or equal to the input version v2
// This is just a wrapper function around GreaterThan and EqualTo
func (v *Version) GreaterOrEqual(v2 Version) bool {
	return !v.LessThan(v2)
}

// GreaterThan checks if it is greater than the input version v2
// Ref: https://semver.org/#spec-item-11
func (v *Version) GreaterThan(v2 Version) bool {
	return !v.LessOrEqual(v2)
}

// isPreEqual checks if preRelease parts of the version is less than the input
// preRelease release is compared lexicographically for simplicity.
func (v *Version) isPreLessThan(pre string) bool {
	return v.preRelease < pre
}

// Raw returns the input version string
func (v *Version) Raw() string {
	return v.raw
}

// genVersionStr generates the version string formatted as SemVer
// It generates string of the format: major.minor.patch-<pre>+<build>
// Here `pre` and `build` parts are assumed to be optional
func (v *Version) genVersionStr() error {
	var buffer bytes.Buffer

	// version = major.minor.patch
	_, err := buffer.WriteString(fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch))
	if err != nil {
		return errors.Wrapf(err, "failed to generate version string with major %q, minor %q and patch %q",
			v.major, v.minor, v.patch)
	}

	// version = major.minior.patch-<pre>
	if v.preRelease != "" {
		_, err = buffer.WriteString("-" + v.preRelease)
		if err != nil {
			return errors.Wrapf(err, "failed to concatenate pre-release %q", v.preRelease)
		}
	}

	// version = major.minor.patch-<pre>+<build>
	if v.build != "" {
		_, err = buffer.WriteString("+" + v.build)
		if err != nil {
			return errors.Wrapf(err, "failed to concatenate build %q", v.build)
		}
	}

	v.versionStr = buffer.String()

	return nil
}

func (v *Version) parseSemVer(str string) error {
	matcher, err := regexp.Compile(RegexSemVer)
	if err != nil {
		return errors.Wrapf(err, "failed to parse semver regex %q", RegexSemVer)
	}

	match := matcher.FindStringSubmatch(str)
	if len(match) == 6 {
		var major, minor, patch uint64
		var pre, suffix string

		major, err = strconv.ParseUint(match[1], 10, strconv.IntSize)
		if err != nil {
			return errors.Wrapf(err, "failed to parse major part %q", match[1])
		}

		minor, err = strconv.ParseUint(match[2], 10, strconv.IntSize)
		if err != nil {
			return errors.Wrapf(err, "failed to parse minor part %q", match[2])
		}

		patch, err = strconv.ParseUint(match[3], 10, strconv.IntSize)
		if err != nil {
			return errors.Wrapf(err, "failed to parse patch part %q", match[3])
		}

		pre = match[4]
		suffix = match[5]

		v.major = major
		v.minor = minor
		v.patch = patch
		v.preRelease = pre
		v.build = suffix

		return v.genVersionStr()
	}

	return errors.Newf("failed to parse version string %q", str)
}

// parse parses the version string into its components
// It allows parsing various formats such as: 219, v1, v1.1, 8.30 and SemVer
func (v *Version) parse(str string) error {
	var err error
	str = strings.TrimLeft(str, "v") // trim "v" prefix

	if str == "" {
		return nil // use default, nothing to parse
	}

	dotOccurrences := strings.Count(str, ".")
	if dotOccurrences == 0 { // just a number
		// support just a number e.g. 219, v1
		v.major, err = strconv.ParseUint(str, 10, strconv.IntSize)
		if err != nil {
			return errors.Wrapf(err, "failed to parse major part %q", str)
		}

		return v.genVersionStr()
	} else if dotOccurrences == 1 { // two parts version
		// support 8.30 or v1.1
		parts := strings.Split(str, ".")
		v.major, err = strconv.ParseUint(parts[0], 10, strconv.IntSize)
		if err != nil {
			return errors.Wrapf(err, "failed to parse major part %q", parts[0])
		}

		v.minor, err = strconv.ParseUint(parts[1], 10, strconv.IntSize)
		if err != nil {
			return errors.Wrapf(err, "failed to parse minor part %q", parts[1])
		}

		return v.genVersionStr()
	}

	// parse assuming a SemVer format
	return v.parseSemVer(str)
}

// NewVersion parses and returns an instance of Version if parsing of the input version string is successful
func NewVersion(v string) (Version, error) {
	v = strings.TrimSpace(v)
	sv := Version{raw: v}
	err := sv.parse(v)
	if err != nil {
		return Version{}, errors.Wrapf(err, "failed to parse version %q", v)
	}

	return sv, nil
}

// CheckVersionRequirements checks if a version is between the minimum and maximum version
// if maximum is empty string, it will ignore checking for that version.
func CheckVersionRequirements(progVersion string, minimum string, maximum string) error {
	pVer, err := NewVersion(progVersion)
	if err != nil {
		return errors.Wrapf(err, "failed to parse program's version string %q", progVersion)
	}

	// check minimum version
	minVer, err := NewVersion(minimum)
	if err != nil {
		return errors.Wrapf(err, "failed to parse minimum version requirement %q", minimum)
	}

	if pVer.LessThan(minVer) {
		return errors.Newf("program version %q is less than minimum required version %q",
			progVersion, minimum)
	}

	// check maximum version
	if maximum != "" {
		maxVer, err := NewVersion(maximum)
		if err != nil {
			return errors.Wrapf(err, "failed to parse maximum version requirement %q", maximum)
		}

		if pVer.GreaterThan(maxVer) {
			return errors.Newf("program version %q is greater than maximum required version %q",
				progVersion, maximum)
		}
	}

	return nil
}

// CheckMinVersionRequirement checks if a version meets the minimum version requirements
func CheckMinVersionRequirement(progVersion string, minimum string) error {
	pVer, err := NewVersion(progVersion)
	if err != nil {
		return errors.Wrapf(err, "failed to parse program's version string %q", progVersion)
	}

	minVer, err := NewVersion(minimum)
	if err != nil {
		return errors.Wrapf(err, "failed to parse minimum version requirements %q", minimum)
	}

	if pVer.LessThan(minVer) {
		return errors.Newf("program version %q is less than minimum required version %q",
			progVersion, minimum)
	}

	return nil
}

// CheckMaxVersionRequirement checks if a version meets the maximum version requirements
func CheckMaxVersionRequirement(progVersion string, maximum string) error {
	pVer, err := NewVersion(progVersion)
	if err != nil {
		return errors.Wrapf(err, "failed to parse program's version string %q", progVersion)
	}

	maxVer, err := NewVersion(maximum)
	if err != nil {
		return errors.Wrapf(err, "failed to parse maximum version requirements %q", maximum)
	}

	if pVer.GreaterThan(maxVer) {
		return errors.Newf("program version %q is greater than maximum required version %q",
			progVersion, maximum)
	}

	return nil
}
