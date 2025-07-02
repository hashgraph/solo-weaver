/*
 * Copyright 2016-2023 Hedera Hashgraph, LLC
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

package software

import (
	"context"
	"github.com/cockroachdb/errors"
	"github.com/joomcode/errorx"
	"github.com/rs/zerolog"
	ver "golang.hedera.com/solo-provisioner/internal/version"
	"golang.hedera.com/solo-provisioner/pkg/detect"
	"golang.hedera.com/solo-provisioner/pkg/security"
	"golang.hedera.com/solo-provisioner/pkg/security/principal"
	"golang.hedera.com/solo-provisioner/pkg/software/specs"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
)

// softwareManager implements Manager interface
type softwareManager struct {
	dm        DefinitionManager
	pm        principal.Manager
	osManager detect.OSManager
	detector  ProgramDetector
	logger    *zerolog.Logger

	availabilityCache sync.Map
}

func (s *softwareManager) IsMandatory(name specs.SoftwareName) bool {
	def, err := s.dm.GetDefinition(name)
	if err != nil {
		return false // if we cannot find any definition we assume it to be optional
	}

	return def.Optional == false
}

func (s *softwareManager) IsAvailable(name specs.SoftwareName) bool {
	if _, found := s.availabilityCache.Load(name); found {
		return true
	}

	return false
}

func (s *softwareManager) verifyHash(ctx context.Context, name specs.SoftwareName, softwareSpec specs.SoftwareSpec,
	versionSpec specs.SoftwareVersionSpec, progInfo ProgramInfo) error {

	if softwareSpec.DisableHashVerification == true {
		s.logger.Info().
			Str(logFields.name, name.String()).
			Str(logFields.path, progInfo.GetPath()).
			Str(logFields.hash, progInfo.GetHash()).
			Str(logFields.expectedHash, versionSpec.Sha256Hash).
			Msg("Software State: Executable Integrity Check Disabled")

		return nil
	}

	if progInfo.GetHash() == versionSpec.Sha256Hash {
		s.logger.Info().
			Str(logFields.name, name.String()).
			Str(logFields.path, progInfo.GetPath()).
			Str(logFields.hash, progInfo.GetHash()).
			Str(logFields.expectedHash, versionSpec.Sha256Hash).
			Msg("Software State: Executable Integrity Verified")

		return nil
	}

	if softwareSpec.RelaxHashVerification == true {
		s.logger.Warn().
			Str(logFields.name, name.String()).
			Str(logFields.path, progInfo.GetPath()).
			Str(logFields.hash, progInfo.GetHash()).
			Str(logFields.expectedHash, versionSpec.Sha256Hash).
			Msg("Software State: Executable Integrity Check Compromised (Safety Check Relaxed)")
		return nil
	} else {
		s.logger.Error().
			Str(logFields.name, name.String()).
			Str(logFields.path, progInfo.GetPath()).
			Str(logFields.hash, progInfo.GetHash()).
			Str(logFields.expectedHash, versionSpec.Sha256Hash).
			Msg("Software State: Executable Integrity Check Compromised")
	}

	return errors.Newf("software executable hash integrity compromised for %q: expected hash %q, found %q",
		progInfo.GetPath(), versionSpec.Sha256Hash, progInfo.GetHash())
}

func (s *softwareManager) verifyVersion(ctx context.Context, name specs.SoftwareName,
	versionRequirement specs.VersionRequirementSpec, versionSpec specs.SoftwareVersionSpec,
	progInfo ProgramInfo) error {

	// store in local variable
	// this helps with mocking and unit testing
	path := progInfo.GetPath()
	version := progInfo.GetVersion()

	// convert to a semver
	progVer, err := ver.NewVersion(version)
	if err != nil {
		return errors.Wrapf(err, "failed to parse program version %q", version)
	}

	// convert to a semver
	minVer, err := ver.NewVersion(versionRequirement.Minimum)
	if err != nil {
		return errors.Wrapf(err, "failed to parse min version %q", versionRequirement.Minimum)
	}

	// convert to a semver
	maxVer, err := ver.NewVersion(versionRequirement.Maximum)
	if err != nil {
		return errors.Wrapf(err, "failed to parse max version %q", versionRequirement.Maximum)
	}

	// check if the required version match with the installed program version
	if versionSpec.Version != "" {

		// convert to a semver
		specVer, err := ver.NewVersion(versionSpec.Version)
		if err != nil {
			return errors.Wrapf(err, "failed to parse spec version %q", versionSpec.Version)
		}

		// check explicit version
		if !specVer.EqualTo(progVer) {
			s.logger.Error().
				Str(logFields.name, name.String()).
				Str(logFields.path, path).
				Str(logFields.version, version).
				Str(logFields.expectedVersion, versionSpec.Version).
				Msg("Software State: Expected Version and Actual Version Mismatch")

			return errors.Newf("software executable version mismatch for %q: expected version %q, found %q",
				path, versionSpec.Version, version)
		}

		s.logger.Info().
			Str(logFields.name, name.String()).
			Str(logFields.path, path).
			Str(logFields.version, version).
			Str(logFields.expectedVersion, versionSpec.Version).
			Msg("Software State: Expected Version and Actual Version Verified")

		return nil
	}

	// check min version bounds
	if progVer.LessThan(minVer) {
		s.logger.Error().
			Str(logFields.name, name.String()).
			Str(logFields.path, path).
			Str(logFields.version, version).
			Str(logFields.minVersion, versionRequirement.Minimum).
			Str(logFields.maxVersion, versionRequirement.Maximum).
			Msg("Software State: Unsupported Executable Version (Version < Minimum)")

		return errors.Newf("program version %q is less than min version %q",
			version, versionRequirement.Maximum)
	}

	// check max version bounds
	if progVer.GreaterThan(maxVer) {
		s.logger.Error().
			Str(logFields.name, name.String()).
			Str(logFields.path, path).
			Str(logFields.version, version).
			Str(logFields.minVersion, versionRequirement.Minimum).
			Str(logFields.maxVersion, versionRequirement.Maximum).
			Msg("Software State: Unsupported Executable Version (Version > Maximum)")

		return errors.Newf("program version %q is greater than max version %q",
			version, versionRequirement.Maximum)
	}

	s.logger.Info().
		Str(logFields.name, name.String()).
		Str(logFields.path, path).
		Str(logFields.version, version).
		Msg("Software State: Executable Version Verified")

	return nil
}

// extractSoftwareSpecForOS returns software spec from the definition based on os type, os flavor and os version
func (s *softwareManager) extractSoftwareSpecForOS(def specs.SoftwareDefinition) (
	specs.SoftwareSpec, error,
) {

	name := def.Executable.Name
	osInfo, err := s.osManager.GetOSInfo()
	osType := specs.OSType(osInfo.Type)
	osVersion := DefaultOSVersion // TODO update detect.OSManager to detect os version
	osFlavor := DefaultOSFlavor   // TODO update detect.OSManager to detect os flavor
	softwareSpec, err := def.GetSoftwareSpec(name, osType, osFlavor, osVersion)
	if err != nil {
		return specs.SoftwareSpec{}, errors.Wrapf(err, "failed to retrieve software spec for %q for OS (%s, %s, %s)",
			name, osType, osFlavor, osVersion)
	}

	return softwareSpec, nil
}

func (s *softwareManager) CheckState(ctx context.Context, def specs.SoftwareDefinition) error {
	name := def.GetName()

	// check cache to see if we have already found it to be available or not
	if s.IsAvailable(name) {
		return nil
	}

	// get os specific software spec
	softwareSpec, err := s.extractSoftwareSpecForOS(def)
	if err != nil {
		return errorx.IllegalArgument.
			New("software spec does not exist for the OS: %s", name).
			WithUnderlyingErrors(err)
	}

	// detect executable program info
	progInfo, err := s.detector.GetProgramInfo(ctx, def.Executable)
	if err != nil {
		return errors.Wrapf(err, "failed to detect executable details for %q", name)
	}

	// find software version spec based on program hash
	versionSpec, err := softwareSpec.GetSoftwareVersionSpec(progInfo.GetVersion())
	if err != nil {
		// try to get the default version spec on error if default version is set
		if softwareSpec.DefaultVersion != "" {
			versionSpec, _ = softwareSpec.GetSoftwareVersionSpec(softwareSpec.DefaultVersion)
			if err != nil {
				return errors.Wrapf(err, "failed to retrieve software version info for %q based on hash %q", name, progInfo.GetHash())
			}
		}
	}

	err = s.verifyVersion(ctx, name, def.Executable.RequiredVersion, versionSpec, progInfo)
	if err != nil {
		return errors.Wrapf(err, "failed to verify version for %q", name)
	}

	err = s.verifyHash(ctx, name, softwareSpec, versionSpec, progInfo)
	if err != nil {
		return errors.Wrapf(err, "failed to verify hash for %q", name)
	}

	// update availabilityCache
	s.availabilityCache.Store(name, progInfo)

	return nil
}

func (s *softwareManager) ScanDependencies(ctx context.Context, definitionList []specs.SoftwareDefinition) error {
	var missing []string
	for _, def := range definitionList {
		name := def.GetName()
		err := s.CheckState(ctx, def)
		if err != nil {
			if s.IsMandatory(name) {
				s.logger.Error().
					Str(logFields.name, name.String()).
					Msg("Scan Software: Required Software Not Available")
				missing = append(missing, name.String())
			}
		}
	}

	if len(missing) > 0 {
		return errors.Newf("software dependencies are not available for: %s", missing)
	}

	return nil
}

func (s *softwareManager) Exec(ctx context.Context, name specs.SoftwareName, args ...string) ([]byte, error) {
	user, err := s.pm.LookupUserByName(security.ServiceAccountUserName)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to lookup user %q", security.ServiceAccountUserName)
	} else if user == nil {
		return nil, errors.Newf("failed to lookup user %q", security.ServiceAccountUserName)
	}

	return s.ExecAsUser(ctx, name, user, args...)
}

func (s *softwareManager) ExecAsUser(ctx context.Context, name specs.SoftwareName, user principal.User, args ...string) ([]byte, error) {
	uid, err := strconv.Atoi(user.Uid())
	if err != nil {
		return nil, errors.Wrapf(err, "failed to convert user ID %q", user.Uid())
	}

	gid, err := strconv.Atoi(user.PrimaryGroup().Gid())
	if err != nil {
		return nil, errors.Wrapf(err, "failed to convert group ID %q", user.PrimaryGroup().Gid())
	}

	cmd := exec.CommandContext(ctx, name.String(), args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid: uint32(uid),
			Gid: uint32(gid),

			// NoSetGroups: this lets us invoke as user without requiring privilege access
			// see: https://github.com/golang/go/commit/79f6a5c7bd684f2e6007ee505b522440beb86bf0
			// see: https://walac.github.io/golang-patch/
			NoSetGroups: true,
		},
	}

	return cmd.Output()
}

// Option allows injecting various parameters for SoftwareManager
type Option = func(sm *softwareManager)

// WithDefinitionManager allows injecting a DefinitionManager for the Manager
func WithDefinitionManager(dm DefinitionManager) Option {
	return func(sm *softwareManager) {
		if dm != nil {
			sm.dm = dm
		}
	}
}

// WithProgramDetector allows injecting a software detector for the Manager
func WithProgramDetector(d ProgramDetector) Option {
	return func(sm *softwareManager) {
		if d != nil {
			sm.detector = d
		}
	}
}

// WithLogger allows injecting a logger for the Manager
func WithLogger(logger *zerolog.Logger) Option {
	return func(sm *softwareManager) {
		if logger != nil {
			sm.logger = logger
			sm.detector.SetLogger(logger)
		}
	}
}

// WithOSManager allows injecting OS manager for the Manager
func WithOSManager(om detect.OSManager) Option {
	return func(sm *softwareManager) {
		if om != nil {
			sm.osManager = om
		}
	}
}

// WithPrincipalManager allows injecting a principal.Manager for the Manager
func WithPrincipalManager(pm principal.Manager) Option {
	return func(sm *softwareManager) {
		if pm != nil {
			sm.pm = pm
		}
	}
}

// NewManager returns an instance of Manager
func NewManager(opts ...Option) (Manager, error) {
	return newSoftwareManager(opts...)
}

func newSoftwareManager(opts ...Option) (*softwareManager, error) {
	logger := zerolog.Nop()
	sm := &softwareManager{
		detector:          NewProgramDetector(&logger),
		availabilityCache: sync.Map{},
		logger:            &logger,
		osManager:         detect.NewOSManager(detect.WithOSManagerLogger(&logger)),
	}

	for _, opt := range opts {
		opt(sm)
	}

	// if it is not set already, instantiate a DefinitionManager
	// we don't initialize the DefinitionManager by default since it tries to load
	// the software definition files on instantiation which is a costly operation
	if sm.dm == nil {
		dm, err := NewDefinitionManager()
		if err != nil {
			return nil, err
		}

		sm.dm = dm
	}

	// if it is not set already, instantiate a principal manager
	// we don't initialize the principal.Manager by default since it tries to load
	// the users and groups on instantiation which is a costly operation
	if sm.pm == nil {
		pm, err := principal.NewManager()
		if err != nil {
			return nil, err
		}

		sm.pm = pm
	}

	return sm, nil
}
