package templates

import (
	"path"
	"strings"

	"github.com/joomcode/errorx"
	"golang.org/x/text/encoding/unicode"
)

func Read(name string) ([]byte, error) {
	if strings.TrimSpace(name) == "" {
		return nil, errorx.IllegalArgument.New("file name cannot be empty")
	}

	data, err := Files.ReadFile(name)
	if err != nil {
		return nil, errorx.DataUnavailable.Wrap(err, "failed to read embedded file %s", name)
	}

	return data, nil
}

func ReadAsString(name string) (string, error) {
	data, err := Read(name)
	if err != nil {
		return "", err // already wrapped
	}

	// validate that the file contents are UTF-8 before casting into string
	utf8Data, err := unicode.UTF8.NewDecoder().Bytes(data)
	if err != nil {
		return "", errorx.IllegalFormat.Wrap(err, "failed to decode file %s as UTF-8", name)
	}

	return string(utf8Data), nil
}

func ReadDir(dir string) ([]string, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errorx.IllegalArgument.New("directory name cannot be empty")
	}

	entries, err := Files.ReadDir(dir)
	if err != nil {
		return nil, errorx.DataUnavailable.Wrap(err, "failed to read embedded directory %s", dir)
	}

	var fileNames []string
	for _, entry := range entries {
		if !entry.IsDir() {
			fileNames = append(fileNames, path.Join(dir, entry.Name()))
		}
	}

	return fileNames, nil
}

// Render renders a template with the given data and returns the result as a string.
// Use: https://github.com/hairyhenderson/gomplate
//func Render()
