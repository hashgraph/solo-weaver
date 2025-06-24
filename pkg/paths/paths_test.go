package paths

import (
	"fmt"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPaths_TrimFromPath(t *testing.T) {
	req := require.New(t)
	loc := filepath.Join("a", "b", "c", "d")
	expected := filepath.Join("a", "b", "c") // only "d" will be trimmed
	actual, err := TrimFromPath(loc, []string{"d", "e"})
	req.NoError(err)
	req.Equal(expected, actual)

	loc = filepath.Join(".", "a", "b", "c", "e", "d")
	expected = filepath.Join(".", "a", "b", "c") // both "d" and "e will be trimmed
	actual, err = TrimFromPath(loc, []string{"d", "e"})
	req.NoError(err)
	req.Equal(expected, actual)

	loc = filepath.Join(".", "a", "b", "c", "e", "d.exe")
	expected = filepath.Join(".", "a", "b", "c") // "e" and "d.exe" will be trimmed
	actual, err = TrimFromPath(loc, []string{"d", "e"})
	req.NoError(err)
	req.Equal(expected, actual)

	// try abs path
	loc = fmt.Sprintf("%s%s", string(os.PathSeparator), filepath.Join(".", "a", "b", "c", "e", "d.exe"))
	expected = fmt.Sprintf("%s%s", string(os.PathSeparator), filepath.Join(".", "a", "b", "c")) // "e" and "d.exe" will be trimmed
	actual, err = TrimFromPath(loc, []string{"d", "e"})
	req.NoError(err)
	req.Equal(expected, actual)

	// error scenario with entire path being excluded
	loc = filepath.Join("d", "e")
	actual, err = TrimFromPath(loc, []string{"d", "e"})
	req.Error(err)
	req.Equal("", actual)
}

func TestPaths_FolderExists(t *testing.T) {
	req := require.New(t)
	tmpDir := os.TempDir()
	invalidFolder := fmt.Sprintf("%s/INVALID/%d", tmpDir, time.Now().Second())
	req.False(FolderExists(invalidFolder))

	validFolder, _ := os.MkdirTemp(tmpDir, "test*")
	defer os.Remove(validFolder)

	req.True(FolderExists(validFolder))
}

func TestPaths_Contains(t *testing.T) {
	req := require.New(t)
	req.True(Contains("a", []string{"z", "b", "a"}))
	req.True(Contains("a", []string{"a", "z", "b"}))
	req.False(Contains("a", []string{"z", "b"}))
	req.False(Contains("a", []string{}))
}

func TestPaths_FindParent(t *testing.T) {
	req := require.New(t)
	// if path ends with the parent name
	result, err := FindParentPath("/a/b/c/d/e", "e")
	req.NoError(err)
	req.Equal("/a/b/c/d/e", result)

	// test immediate parent
	result, err = FindParentPath("/a/b/c/d/e", "d")
	req.NoError(err)
	req.Equal("/a/b/c/d", result)

	// intermediate parent
	result, err = FindParentPath("/a/b/c/d/e", "c")
	req.NoError(err)
	req.Equal("/a/b/c", result)

	// root parent
	result, err = FindParentPath("/a/b/c/d/e", "a")
	req.NoError(err)
	req.Equal("/a", result)

	// no matching parent
	result, err = FindParentPath("/a/b/c/d/e", "INVALID")
	req.Error(err)
	req.Empty(result)
}

func TestPaths_FindChildPath(t *testing.T) {
	req := require.New(t)
	rootDir := mockTempDirRoot()
	// one level deep
	result, err := FindChildPath(rootDir, "hgcapp", DefaultMaxDepth)
	req.NoError(err)
	req.Equal(filepath.Join(rootDir, "hgcapp"), result)

	// two level deep
	result, err = FindChildPath(rootDir, "solo-provisioner", DefaultMaxDepth)
	req.NoError(err)
	req.Equal(filepath.Join(rootDir, "hgcapp/solo-provisioner"), result)

	// first matching directory, several level deep
	tmpCurrentDir := filepath.Join(rootDir, "/hgcapp/solo-provisioner/upgrade/current")
	os.Mkdir(tmpCurrentDir, DefaultDirMode)
	result, err = FindChildPath(rootDir, "current", DefaultMaxDepth)
	req.NoError(err)
	req.Equal(tmpCurrentDir, result)

	// matching a sub path
	result, err = FindChildPath(rootDir, "/data/upgrade/current", DefaultMaxDepth)
	req.NoError(err)
	req.Equal(filepath.Join(rootDir, "/hgcapp/services-hedera/H1/data/upgrade/current"), result) // first symlink

	// error for limited maxDepth
	result, err = FindChildPath(rootDir, "solo-provisioner", 1)
	req.Error(err)
	req.Empty(result)
}

func TestPaths_Parent(t *testing.T) {
	req := require.New(t)
	root := string(os.PathSeparator)
	req.Equal(root, Parent(""))
	req.Equal(root, Parent(string(os.PathSeparator)))
	req.Equal(root, Parent(filepath.Join("a")))
	req.Equal(root, Parent(filepath.Join(root, "a")))
	req.Equal(filepath.Join(root, "a"), Parent(filepath.Join(root, "a", "b")))
}
