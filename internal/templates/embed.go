package templates

import (
	"embed"
	"github.com/joomcode/errorx"
	"golang.org/x/text/encoding/unicode"
	"strings"
)

//go:embed files/*
var Files embed.FS

func ReadAsString(name string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", errorx.IllegalArgument.New("file name cannot be empty")
	}

	data, err := Files.ReadFile(name)
	if err != nil {
		return "", errorx.DataUnavailable.Wrap(err, "failed to read embedded file %s", name)
	}

	// validate that the file contents are UTF-8 before casting into string
	utf8Data, err := unicode.UTF8.NewDecoder().Bytes(data)
	if err != nil {
		return "", errorx.IllegalFormat.Wrap(err, "failed to decode file %s as UTF-8", name)
	}

	return string(utf8Data), nil
}

// Render renders a template with the given data and returns the result as a string.
// Use: https://github.com/hairyhenderson/gomplate
//func Render()
