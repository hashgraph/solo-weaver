// SPDX-License-Identifier: Apache-2.0

package templates

import (
	"path"
	"strings"
	"text/template"

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
func Render(templateSrc string, data any) (string, error) {
	// Read the template file
	templateContent, err := ReadAsString(templateSrc)
	if err != nil {
		return "", errorx.DataUnavailable.Wrap(err, "failed to read template file %s", templateSrc)
	}

	// Create a new template and parse the content
	tmpl, err := template.New("template").Parse(templateContent)
	if err != nil {
		return "", errorx.IllegalFormat.Wrap(err, "failed to parse template %s", templateSrc)
	}

	// Create a builder to capture the output
	var builder strings.Builder

	// Execute the template with the provided data
	err = tmpl.Execute(&builder, data)
	if err != nil {
		return "", errorx.IllegalState.Wrap(err, "failed to execute template %s", templateSrc)
	}

	// Return the rendered template as a string
	return builder.String(), nil
}
