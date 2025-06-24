package backup

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"github.com/rs/zerolog"
	"os"
	"os/user"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-provisioner/pkg/fsx"
	"golang.hedera.com/solo-provisioner/pkg/security/principal"
)

func TestUnixManager_NewManager(t *testing.T) {
	req := require.New(t)

	cfg := &Backup{
		Pruning: Pruning{
			MaxAge:    30,
			MaxCopies: 11,
		},
	}

	req.NotNil(cfg)
	req.NotNil(cfg.Pruning)
	req.Equal(30, cfg.Pruning.MaxAge)
	req.Equal(11, cfg.Pruning.MaxCopies)

	bkp, err := NewManager()
	req.NoError(err)
	req.NotNil(bkp)
	req.NotNil(bkp.(*unixManager).pm)
	req.NotNil(bkp.(*unixManager).fs)
	req.NotNil(bkp.(*unixManager).cfg)
	req.NotNil(bkp.(*unixManager).cfg.Pruning)
	req.Equal(-1, bkp.(*unixManager).cfg.Pruning.MaxAge)
	req.Equal(-1, bkp.(*unixManager).cfg.Pruning.MaxCopies)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	pm := setupMockPrincipalManager(t, ctrl)

	fsm, err := fsx.NewManager(fsx.WithPrincipalManager(pm))
	req.NoError(err)
	req.NotNil(fsm)

	bkp, err = NewManager(WithPrincipalManager(pm), WithFileSystemManager(fsm))
	req.NoError(err)
	req.NotNil(bkp)
	req.NotNil(bkp.(*unixManager).pm)
	req.NotNil(bkp.(*unixManager).fs)

	bkp, err = NewManager(WithPrincipalManager(pm))
	req.NoError(err)
	req.NotNil(bkp)
	req.NotNil(bkp.(*unixManager).pm)
	req.NotNil(bkp.(*unixManager).fs)

	bkp, err = NewManager(WithPrincipalManager(pm), WithFileSystemManager(fsm), WithConfiguration(cfg))
	req.NoError(err)
	req.NotNil(bkp)
	req.NotNil(bkp.(*unixManager).pm)
	req.NotNil(bkp.(*unixManager).fs)
	req.NotNil(bkp.(*unixManager).cfg)
	req.NotNil(bkp.(*unixManager).cfg.Pruning)
	req.Equal(30, bkp.(*unixManager).cfg.Pruning.MaxAge)
	req.Equal(11, bkp.(*unixManager).cfg.Pruning.MaxCopies)

	logger := zerolog.Nop()
	bkp, err = NewManager(
		WithPrincipalManager(pm),
		WithFileSystemManager(fsm),
		WithConfiguration(cfg),
		WithLogger(&logger),
	)
	req.NoError(err)
	req.NotNil(bkp)
	req.NotNil(bkp.(*unixManager).pm)
	req.NotNil(bkp.(*unixManager).fs)
	req.NotNil(bkp.(*unixManager).cfg)
	req.Equal(logger, bkp.(*unixManager).logger)

	// failures with options passing nil pointers
	bkp, err = NewManager(WithPrincipalManager(nil))
	req.Error(err)
	req.Nil(bkp)

	bkp, err = NewManager(WithConfiguration(nil))
	req.Error(err)
	req.Nil(bkp)

	bkp, err = NewManager(WithFileSystemManager(nil))
	req.Error(err)
	req.Nil(bkp)

	bkp, err = NewManager(WithLogger(&logger))
	req.Error(err)
	req.Nil(bkp)
}

func TestUnixManager_IsVersioned(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	pm := setupMockPrincipalManager(t, ctrl)

	req, bkp, ts := setupTest(t, pm, nil)

	req.True(bkp.IsVersioned(ts.FileSymlink))
	req.True(bkp.IsVersioned(ts.DirectorySymlink))
	req.False(bkp.IsVersioned(ts.NormalFile))
	req.False(bkp.IsVersioned(ts.NormalDirectory))
	req.False(bkp.IsVersioned(ts.VersionedFile))
	req.False(bkp.IsVersioned(ts.VersionedDirectory))
}

func TestUnixManager_CurrentVersion(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	pm := setupMockPrincipalManager(t, ctrl)

	req, bkp, ts := setupTest(t, pm, nil)

	cfv, err := bkp.CurrentVersion(ts.FileSymlink)
	req.NoError(err)
	req.Equal(filepath.Dir(ts.FileSymlink), cfv.RootPath)
	req.Equal(filepath.Base(ts.FileSymlink), cfv.Name)
	req.Equal(ts.VersionTime, cfv.Date)
	req.Equal(ts.VersionedFile, cfv.Path)
	req.True(cfv.IsActive)

	dfv, err := bkp.CurrentVersion(ts.DirectorySymlink)
	req.NoError(err)
	req.Equal(filepath.Dir(ts.DirectorySymlink), dfv.RootPath)
	req.Equal(filepath.Base(ts.DirectorySymlink), dfv.Name)
	req.Equal(ts.VersionTime, dfv.Date)
	req.Equal(ts.VersionedDirectory, dfv.Path)
	req.True(dfv.IsActive)

	_, err = bkp.CurrentVersion(ts.NormalFile)
	req.Error(err)

	_, err = bkp.CurrentVersion(ts.NormalDirectory)
	req.Error(err)

	_, err = bkp.CurrentVersion(ts.VersionedFile)
	req.Error(err)

	_, err = bkp.CurrentVersion(ts.VersionedDirectory)
	req.Error(err)

	_, err = bkp.CurrentVersion("/does/not/exist")
	req.Error(err)

	// Check the case when the symlink is broken
	err = os.Remove(ts.VersionedFile)
	req.NoError(err)

	_, err = bkp.CurrentVersion(ts.FileSymlink)
	req.Error(err)

	// Check the case when the datetime is invalid
	_, err = bkp.CurrentVersion(ts.BadTimeFileSymlink)
	req.Error(err)

	_, err = bkp.CurrentVersion(ts.BadTimeDirectorySymlink)
	req.Error(err)

	// Check the case when the file name is invalid
	_, err = bkp.CurrentVersion(ts.BadNameFileSymlink)
	req.Error(err)

	_, err = bkp.CurrentVersion(ts.BadNameDirectorySymlink)
	req.Error(err)
}

func TestUnixManager_EnumerateVersions(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	pm := setupMockPrincipalManager(t, ctrl)

	req, bkp, ts := setupTest(t, pm, nil)

	versions, err := bkp.EnumerateVersions(ts.FileSymlink)
	req.NoError(err)
	req.Len(versions, 1)
	req.Equal(filepath.Dir(ts.FileSymlink), versions[0].RootPath)
	req.Equal(filepath.Base(ts.FileSymlink), versions[0].Name)
	req.True(versions[0].IsActive)
	req.Equal(ts.VersionTime, versions[0].Date)

	versions, err = bkp.EnumerateVersions(ts.DirectorySymlink)
	req.NoError(err)
	req.Len(versions, 1)
	req.Equal(filepath.Dir(ts.DirectorySymlink), versions[0].RootPath)
	req.Equal(filepath.Base(ts.DirectorySymlink), versions[0].Name)
	req.True(versions[0].IsActive)
	req.Equal(ts.VersionTime, versions[0].Date)

	addAdditionalBackups(t, req, ts, filepath.Base(ts.FileSymlink), 10, false)
	addAdditionalBackups(t, req, ts, filepath.Base(ts.DirectorySymlink), 5, true)

	versions, err = bkp.EnumerateVersions(ts.FileSymlink)
	req.NoError(err)
	req.Len(versions, 11)
	req.Equal(filepath.Dir(ts.FileSymlink), versions[0].RootPath)
	req.Equal(filepath.Base(ts.FileSymlink), versions[0].Name)
	req.True(versions[0].IsActive)
	req.Equal(ts.VersionTime, versions[0].Date)

	for i := 1; i < len(versions); i++ {
		req.Equal(filepath.Dir(ts.FileSymlink), versions[i].RootPath)
		req.Equal(filepath.Base(ts.FileSymlink), versions[i].Name)
		req.False(versions[i].IsActive)
	}

	versions, err = bkp.EnumerateVersions(ts.DirectorySymlink)
	req.NoError(err)
	req.Len(versions, 6)
	req.Equal(filepath.Dir(ts.DirectorySymlink), versions[0].RootPath)
	req.Equal(filepath.Base(ts.DirectorySymlink), versions[0].Name)
	req.True(versions[0].IsActive)
	req.Equal(ts.VersionTime, versions[0].Date)

	for i := 1; i < len(versions); i++ {
		req.Equal(filepath.Dir(ts.DirectorySymlink), versions[i].RootPath)
		req.Equal(filepath.Base(ts.DirectorySymlink), versions[i].Name)
		req.False(versions[i].IsActive)
	}
}

func TestUnixManager_CreateVersion_File(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	pm := setupMockPrincipalManager(t, ctrl)

	req, bkp, ts := setupTest(t, pm, nil)

	buff := make([]byte, 1024)
	_, err := rand.Read(buff)
	req.NoError(err)

	origHash := sha256.Sum256(buff)
	err = os.WriteFile(ts.VersionedFile, buff, 0644)
	req.NoError(err)

	v, err := bkp.CreateVersion(ts.FileSymlink)
	req.NoError(err)
	req.NotNil(v)
	req.Equal(filepath.Dir(ts.FileSymlink), v.RootPath)
	req.Equal(filepath.Base(ts.FileSymlink), v.Name)
	req.True(v.IsActive)

	cvi, err := bkp.CurrentVersion(ts.FileSymlink)
	req.NoError(err)
	req.NotNil(cvi)
	req.Equal(v.Path, cvi.Path)

	fc, err := os.ReadFile(v.Path)
	req.NoError(err)
	req.NotNil(fc)

	newHash := sha256.Sum256(fc)
	req.Equal(origHash, newHash)
}

func TestUnixManager_CreateVersion_Directory(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	pm := setupMockPrincipalManager(t, ctrl)

	req, bkp, ts := setupTest(t, pm, nil)

	ovi, err := bkp.CurrentVersion(ts.DirectorySymlink)
	req.NoError(err)

	addDirectoryStructure(t, req, ovi)
	assertTestDirectoryStructure(t, req, bkp.(*unixManager).fs, ovi, false)

	v, err := bkp.CreateVersion(ts.DirectorySymlink)
	req.NoError(err)
	req.NotNil(v)
	req.Equal(filepath.Dir(ts.DirectorySymlink), v.RootPath)
	req.Equal(filepath.Base(ts.DirectorySymlink), v.Name)
	req.True(v.IsActive)

	cvi, err := bkp.CurrentVersion(ts.DirectorySymlink)
	req.NoError(err)
	req.NotNil(cvi)
	req.Equal(v.Path, cvi.Path)
	req.Equal(v.Name, cvi.Name)
	req.Equal(v.RootPath, cvi.RootPath)
	req.Equal(v.Date, cvi.Date)

	req.NotEqual(ovi.Path, cvi.Path)
	req.NotEqual(ovi.Date, cvi.Date)
	req.Equal(ovi.Name, cvi.Name)
	req.Equal(ovi.RootPath, cvi.RootPath)

	assertTestDirectoryStructure(t, req, bkp.(*unixManager).fs, cvi, false)
	assertTestDirectoryStructure(t, req, bkp.(*unixManager).fs, ovi, false)
}

func TestUnixManager_CreateVersion_FileAtTime(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	pm := setupMockPrincipalManager(t, ctrl)

	req, bkp, ts := setupTest(t, pm, nil)

	backupTime := ts.VersionTime.Add(time.Second)

	buff := make([]byte, 1024)
	_, err := rand.Read(buff)
	req.NoError(err)

	origHash := sha256.Sum256(buff)
	err = os.WriteFile(ts.VersionedFile, buff, 0644)
	req.NoError(err)

	v, err := bkp.CreateVersionAt(ts.FileSymlink, backupTime)
	req.NoError(err)
	req.NotNil(v)
	req.Equal(filepath.Dir(ts.FileSymlink), v.RootPath)
	req.Equal(filepath.Base(ts.FileSymlink), v.Name)
	req.True(v.IsActive)

	cvi, err := bkp.CurrentVersion(ts.FileSymlink)
	req.NoError(err)
	req.NotNil(cvi)
	req.Equal(v.Path, cvi.Path)

	fc, err := os.ReadFile(v.Path)
	req.NoError(err)
	req.NotNil(fc)

	newHash := sha256.Sum256(fc)
	req.Equal(origHash, newHash)

	v, err = bkp.CreateVersionAt(ts.FileSymlink, backupTime)
	req.Error(err)
}

func TestUnixManager_DeleteVersion(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	pm := setupMockPrincipalManager(t, ctrl)

	req, bkp, ts := setupTest(t, pm, nil)

	addAdditionalBackups(t, req, ts, filepath.Base(ts.FileSymlink), 10, false)
	addAdditionalBackups(t, req, ts, filepath.Base(ts.DirectorySymlink), 5, true)

	versions, err := bkp.EnumerateVersions(ts.FileSymlink)
	req.NoError(err)
	req.Len(versions, 11)
	req.Equal(filepath.Dir(ts.FileSymlink), versions[0].RootPath)
	req.Equal(filepath.Base(ts.FileSymlink), versions[0].Name)
	req.True(versions[0].IsActive)
	req.Equal(ts.VersionTime, versions[0].Date)

	req.FileExists(versions[1].Path)
	err = bkp.DeleteVersion(versions[1])
	req.NoError(err)
	req.NoFileExists(versions[1].Path)
	req.FileExists(versions[0].Path)
	req.FileExists(filepath.Join(versions[0].RootPath, versions[0].Name))

	versions, err = bkp.EnumerateVersions(ts.DirectorySymlink)
	req.NoError(err)
	req.Equal(versions[0].Path, versions[0].Path)
	req.Equal(versions[0].RootPath, versions[0].RootPath)
	req.Equal(versions[0].Name, versions[0].Name)
	req.Equal(versions[0].Date, versions[0].Date)
	req.True(versions[0].IsActive)

	req.DirExists(versions[1].Path)
	err = bkp.DeleteVersion(versions[1])
	req.NoError(err)
	req.NoDirExists(versions[1].Path)
	req.DirExists(versions[0].Path)
	req.FileExists(filepath.Join(versions[0].RootPath, versions[0].Name))
}

func TestUnixManager_CreateVersion_Directory_WithSnapshotSupport(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	pm := setupMockPrincipalManager(t, ctrl)

	req, bkp, ts := setupTest(t, pm, nil)

	_, err := bkp.CurrentVersion(ts.NormalDirectory)
	req.Error(err)

	req.NoError(os.MkdirAll(filepath.Join(ts.NormalDirectory, "subdir1"), 0755))
	req.NoError(os.MkdirAll(filepath.Join(ts.NormalDirectory, "subdir2"), 0755))
	req.NoError(os.MkdirAll(filepath.Join(ts.NormalDirectory, "subdir1", "subdir3"), 0755))

	v, err := bkp.CreateVersion(ts.NormalDirectory)
	req.NoError(err)
	req.True(v.IsActive)
	req.DirExists(filepath.Join(v.Path, "subdir1"))
	req.DirExists(filepath.Join(v.Path, "subdir1", "subdir3"))
	req.DirExists(filepath.Join(v.Path, "subdir2"))

	v2, err := bkp.CurrentVersion(ts.NormalDirectory)
	req.NoError(err)
	req.Equal(v, v2)

	// fail with invalid directory
	_, err = bkp.CreateVersion(filepath.Join(ts.NormalDirectory, "INVALID"))
	req.Error(err)
	req.Contains(err.Error(), "failed to enable snapshot support to the target path")
}

func TestUnixManager_CreateVersion_File_WithSnapshotSupport(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	pm := setupMockPrincipalManager(t, ctrl)

	req, bkp, ts := setupTest(t, pm, nil)

	_, err := bkp.CurrentVersion(ts.NormalFile)
	req.Error(err)

	v, err := bkp.CreateVersion(ts.NormalFile)
	req.NoError(err)
	req.True(v.IsActive)

	v2, err := bkp.CurrentVersion(ts.NormalFile)
	req.NoError(err)
	req.Equal(v, v2)

	// fail with invalid path
	_, err = bkp.CreateVersion(filepath.Join(ts.NormalDirectory, "INVALID.txt"))
	req.Error(err)
	req.Contains(err.Error(), "failed to enable snapshot support to the target path")
}

func TestUnixManager_CreateVersionWithLogging(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	pm := setupMockPrincipalManager(t, ctrl)

	logger := zerolog.Nop()
	req, bkp, ts := setupTest(t, pm, &logger)

	_, err := bkp.CurrentVersion(ts.NormalDirectory)
	req.Error(err)

	req.NoError(os.MkdirAll(filepath.Join(ts.NormalDirectory, "subdir1"), 0755))
	req.NoError(os.MkdirAll(filepath.Join(ts.NormalDirectory, "subdir2"), 0755))
	req.NoError(os.MkdirAll(filepath.Join(ts.NormalDirectory, "subdir1", "subdir3"), 0755))

	v, err := bkp.CreateVersion(ts.NormalDirectory)
	req.NoError(err)
	req.True(v.IsActive)
	req.DirExists(filepath.Join(v.Path, "subdir1"))
	req.DirExists(filepath.Join(v.Path, "subdir1", "subdir3"))
	req.DirExists(filepath.Join(v.Path, "subdir2"))

	// fail with invalid directory
	_, err = bkp.CreateVersion(filepath.Join(ts.NormalDirectory, "INVALID"))
	req.Error(err)
	req.Contains(err.Error(), "failed to enable snapshot support to the target path")
}

type testStructures struct {
	VersionTime             time.Time
	TempDir                 string
	NormalDirectory         string
	VersionedDirectory      string
	BadTimeDirectory        string
	BadNameDirectory        string
	NormalFile              string
	VersionedFile           string
	BadTimeFile             string
	BadNameFile             string
	FileSymlink             string
	BadTimeFileSymlink      string
	BadNameFileSymlink      string
	DirectorySymlink        string
	BadTimeDirectorySymlink string
	BadNameDirectorySymlink string
}

func setupTest(t *testing.T, pm principal.Manager, logger *zerolog.Logger) (*require.Assertions, Manager, *testStructures) {
	t.Helper()
	req := require.New(t)

	l := zerolog.Nop()
	if logger == nil {
		logger = &l
	}

	ts := &testStructures{}
	ts.VersionTime = time.Date(2023, 4, 17, 14, 48, 52, 0, time.UTC)
	ts.TempDir = t.TempDir()
	ts.NormalDirectory = filepath.Join(ts.TempDir, "normal-directory")
	ts.VersionedDirectory = filepath.Join(ts.TempDir, "HapiApp2.0-20230417T144852")
	ts.BadTimeDirectory = filepath.Join(ts.TempDir, "bad-time-directory-20230417T344852")
	ts.BadNameDirectory = filepath.Join(ts.TempDir, "bad-name-directory-20230417")
	ts.NormalFile = filepath.Join(ts.TempDir, "normal-file")
	ts.VersionedFile = filepath.Join(ts.TempDir, "solo-provisioner-20230417T144852")
	ts.BadTimeFile = filepath.Join(ts.TempDir, "bad-time-file-20230417T344852")
	ts.BadNameFile = filepath.Join(ts.TempDir, "bad-name-file-20230417")
	ts.FileSymlink = filepath.Join(ts.TempDir, "solo-provisioner")
	ts.BadTimeFileSymlink = filepath.Join(ts.TempDir, "bad-time-file")
	ts.BadNameFileSymlink = filepath.Join(ts.TempDir, "bad-name-file")
	ts.DirectorySymlink = filepath.Join(ts.TempDir, "HapiApp2.0")
	ts.BadTimeDirectorySymlink = filepath.Join(ts.TempDir, "bad-time-directory")
	ts.BadNameDirectorySymlink = filepath.Join(ts.TempDir, "bad-name-directory")

	req.NoError(os.MkdirAll(ts.NormalDirectory, 0755))
	req.NoError(os.MkdirAll(ts.VersionedDirectory, 0755))
	req.NoError(os.MkdirAll(ts.BadTimeDirectory, 0755))
	req.NoError(os.MkdirAll(ts.BadNameDirectory, 0755))
	req.NoError(os.WriteFile(ts.NormalFile, make([]byte, 0), 0644))
	req.NoError(os.WriteFile(ts.VersionedFile, make([]byte, 0), 0644))
	req.NoError(os.WriteFile(ts.BadTimeFile, make([]byte, 0), 0644))
	req.NoError(os.WriteFile(ts.BadNameFile, make([]byte, 0), 0644))
	req.NoError(os.Symlink(ts.VersionedFile, ts.FileSymlink))
	req.NoError(os.Symlink(ts.VersionedDirectory, ts.DirectorySymlink))
	req.NoError(os.Symlink(ts.BadTimeFile, ts.BadTimeFileSymlink))
	req.NoError(os.Symlink(ts.BadNameFile, ts.BadNameFileSymlink))
	req.NoError(os.Symlink(ts.BadTimeDirectory, ts.BadTimeDirectorySymlink))
	req.NoError(os.Symlink(ts.BadNameDirectory, ts.BadNameDirectorySymlink))

	fsm, err := fsx.NewManager(fsx.WithPrincipalManager(pm))
	req.NoError(err)
	req.NotNil(fsm)

	bkp, err := NewManager(WithPrincipalManager(pm), WithFileSystemManager(fsm), WithLogger(logger))
	req.NoError(err)
	req.NotNil(bkp)

	return req, bkp, ts
}

func setupMockPrincipalManager(t *testing.T, ctrl *gomock.Controller) principal.Manager {
	pm := principal.NewMockManager(ctrl)

	u, err := user.Current()
	assert.NoError(t, err)

	um := principal.NewMockUser(ctrl)
	group := principal.NewMockGroup(ctrl)
	um.EXPECT().Uid().Return(u.Uid).AnyTimes()
	group.EXPECT().Gid().Return(u.Gid).AnyTimes()
	um.EXPECT().PrimaryGroup().Return(group).AnyTimes()

	pm.EXPECT().LookupUserById(u.Uid).Return(um, nil).AnyTimes()
	pm.EXPECT().LookupGroupById(u.Gid).Return(group, nil).AnyTimes()

	return pm
}

func addAdditionalBackups(t *testing.T, req *require.Assertions, ts *testStructures, linkName string, count int, directory bool) {
	t.Helper()
	for i := 1; i <= count; i++ {
		versionTime := ts.VersionTime.Add(time.Duration(i) * time.Second)
		versionName := fmt.Sprintf("%s-%s", linkName, versionTime.Format("20060102T150405"))
		versionPath := filepath.Join(ts.TempDir, versionName)
		if directory {
			req.NoError(os.MkdirAll(versionPath, 0755))
		} else {
			req.NoError(os.WriteFile(versionPath, make([]byte, 0), 0644))
		}
	}
}

func addDirectoryStructure(t *testing.T, req *require.Assertions, version *Version) {
	t.Helper()
	req.DirExists(version.Path)
	req.NoError(os.MkdirAll(filepath.Join(version.Path, "subdir1"), 0755))
	req.NoError(os.MkdirAll(filepath.Join(version.Path, "subdir2"), 0755))
	req.NoError(os.WriteFile(filepath.Join(version.Path, "subdir1", "file1"), make([]byte, 0), 0644))
	req.NoError(os.WriteFile(filepath.Join(version.Path, "subdir1", "file2"), make([]byte, 0), 0644))
	req.NoError(os.WriteFile(filepath.Join(version.Path, "subdir2", "file3"), make([]byte, 0), 0644))
	req.NoError(os.WriteFile(filepath.Join(version.Path, "subdir2", "file4"), make([]byte, 0), 0644))
	req.NoError(os.Symlink(filepath.Join(version.Path, "subdir1", "file1"), filepath.Join(version.Path, "subdir1", "file1-symlink")))
	req.NoError(os.Symlink("file2", filepath.Join(version.Path, "subdir1", "file2-symlink")))
	req.NoError(os.WriteFile(filepath.Join(version.Path, "file5"), make([]byte, 0), 0644))
	req.NoError(os.Link(filepath.Join(version.Path, "file5"), filepath.Join(version.Path, "file5-hardlink")))
}

func assertTestDirectoryStructure(t *testing.T, req *require.Assertions, fsm fsx.Manager, version *Version, symlinksAsFiles bool) {
	t.Helper()
	req.DirExists(version.Path)
	req.DirExists(filepath.Join(version.Path, "subdir1"))
	req.DirExists(filepath.Join(version.Path, "subdir2"))
	req.FileExists(filepath.Join(version.Path, "subdir1", "file1"))
	req.FileExists(filepath.Join(version.Path, "subdir1", "file2"))
	req.FileExists(filepath.Join(version.Path, "subdir2", "file3"))
	req.FileExists(filepath.Join(version.Path, "subdir2", "file4"))
	req.FileExists(filepath.Join(version.Path, "subdir1", "file1-symlink"))
	req.FileExists(filepath.Join(version.Path, "subdir1", "file2-symlink"))
	if !symlinksAsFiles {
		req.True(fsm.IsSymbolicLink(filepath.Join(version.Path, "subdir1", "file1-symlink")))
		req.True(fsm.IsSymbolicLink(filepath.Join(version.Path, "subdir1", "file2-symlink")))
	} else {
		req.False(fsm.IsSymbolicLink(filepath.Join(version.Path, "subdir1", "file1-symlink")))
		req.True(fsm.IsRegularFile(filepath.Join(version.Path, "subdir1", "file1-symlink")))
		req.False(fsm.IsSymbolicLink(filepath.Join(version.Path, "subdir1", "file2-symlink")))
		req.True(fsm.IsRegularFile(filepath.Join(version.Path, "subdir1", "file2-symlink")))
	}
	req.FileExists(filepath.Join(version.Path, "file5"))
	req.FileExists(filepath.Join(version.Path, "file5-hardlink"))
	req.True(fsm.IsHardLink(filepath.Join(version.Path, "file5-hardlink")))
}

func TestUnixManager_CopyTree(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	pm := setupMockPrincipalManager(t, ctrl)

	req, bkp, ts := setupTest(t, pm, nil)

	req.NoError(os.MkdirAll(filepath.Join(ts.NormalDirectory, "subdir1"), 0755))
	req.NoError(os.MkdirAll(filepath.Join(ts.NormalDirectory, "subdir2"), 0755))
	req.NoError(os.MkdirAll(filepath.Join(ts.NormalDirectory, "subdir1", "subdir3"), 0755))

	destDir := ts.TempDir
	err := bkp.CopyTree(ts.NormalDirectory, destDir)
	req.NoError(err)
	req.DirExists(filepath.Join(destDir, "subdir1"))
	req.DirExists(filepath.Join(destDir, "subdir1", "subdir3"))
	req.DirExists(filepath.Join(destDir, "subdir2"))
}

func TestUnixManager_CopyTreeByFilter(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	pm := setupMockPrincipalManager(t, ctrl)

	req, bkp, ts := setupTest(t, pm, nil)

	req.NoError(os.MkdirAll(filepath.Join(ts.NormalDirectory, "subdir1"), 0755))
	req.NoError(os.MkdirAll(filepath.Join(ts.NormalDirectory, "subdir2"), 0755))
	req.NoError(os.MkdirAll(filepath.Join(ts.NormalDirectory, "subdir1", "subdir3"), 0755))
	req.NoError(os.WriteFile(filepath.Join(ts.NormalDirectory, "file3.pfx"), make([]byte, 0), 0644))
	req.NoError(os.WriteFile(filepath.Join(ts.NormalDirectory, "file3.key"), make([]byte, 0), 0644))

	destDir := ts.TempDir
	filesystemManager, err := fsx.NewManager()
	req.NoError(err)
	filter, err := NewDirAndPFXFilter(filesystemManager, ts.NormalDirectory, nil)
	req.NoError(err)
	err = bkp.CopyTreeByFilter(ts.NormalDirectory, destDir, filter)
	req.NoError(err)
	req.NoDirExists(filepath.Join(destDir, "subdir1"))
	req.NoDirExists(filepath.Join(destDir, "subdir1", "subdir3"))
	req.NoDirExists(filepath.Join(destDir, "subdir2"))
	req.NoFileExists(filepath.Join(destDir, "file3.pfx"))
	req.FileExists(filepath.Join(destDir, "file3.key"))
}
