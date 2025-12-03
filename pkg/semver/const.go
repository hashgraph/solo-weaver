// SPDX-License-Identifier: Apache-2.0

package semver

// RegexSemVer represents version of SevVer format: major.minor.patch-<pre>+<build>
const RegexSemVer = "([0-9]+)\\.([0-9]+)\\.([0-9]+)[-_]?([a-zA-Z0-9\\.]*)\\+?([a-zA-Z0-9]+)?"
