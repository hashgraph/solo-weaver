// SPDX-License-Identifier: Apache-2.0

package hardware

func profilePredicate(p string) func(DeploymentSpec) bool {
	return func(s DeploymentSpec) bool { return s.Profile == p }
}

func presetPredicate(p string) func(DeploymentSpec) bool {
	return func(s DeploymentSpec) bool {
		v, _ := s.Options["preset"].(string)
		return v == p
	}
}

func hasPlugin(name string) func(DeploymentSpec) bool {
	return func(s DeploymentSpec) bool {
		plugins, _ := s.Options["plugins"].([]string)
		for _, pl := range plugins {
			if pl == name {
				return true
			}
		}
		return false
	}
}

func anyProfile(profiles ...string) func(DeploymentSpec) bool {
	return func(s DeploymentSpec) bool {
		for _, p := range profiles {
			if s.Profile == p {
				return true
			}
		}
		return false
	}
}

func and(fns ...func(DeploymentSpec) bool) func(DeploymentSpec) bool {
	return func(s DeploymentSpec) bool {
		for _, fn := range fns {
			if !fn(s) {
				return false
			}
		}
		return true
	}
}

func or(fns ...func(DeploymentSpec) bool) func(DeploymentSpec) bool {
	return func(s DeploymentSpec) bool {
		for _, fn := range fns {
			if fn(s) {
				return true
			}
		}
		return false
	}
}

func not(fn func(DeploymentSpec) bool) func(DeploymentSpec) bool {
	return func(s DeploymentSpec) bool { return !fn(s) }
}

func always() func(DeploymentSpec) bool {
	return func(_ DeploymentSpec) bool { return true }
}
