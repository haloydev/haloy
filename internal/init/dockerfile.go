package init

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/haloydev/haloy/internal/embed"
)

// DockerfileData contains all template variables for Dockerfile generation
type DockerfileData struct {
	// Node.js specific
	NodeVersion    string
	PackageManager string
	InstallCmd     string
	BuildCmd       string
	StartCmd       string
	LockFile       string

	// Python specific
	PythonVersion string

	// Django specific
	ProjectName string

	// General
	Port string
}

// GenerateDockerfile generates a Dockerfile for the given framework
func GenerateDockerfile(framework Framework, data DockerfileData) ([]byte, error) {
	templateFile := framework.TemplateFile()
	if templateFile == "" {
		return nil, fmt.Errorf("no template available for framework: %s", framework)
	}

	templatePath := fmt.Sprintf("dockerfiles/%s", templateFile)
	templateContent, err := embed.DockerfilesFS.ReadFile(templatePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read template %s: %w", templatePath, err)
	}

	tmpl, err := template.New(templateFile).Parse(string(templateContent))
	if err != nil {
		return nil, fmt.Errorf("failed to parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.Bytes(), nil
}

// BuildDockerfileData builds the DockerfileData for a given framework and project
func BuildDockerfileData(framework Framework, projectDir string) DockerfileData {
	data := DockerfileData{
		Port: framework.DefaultPort(),
	}

	switch framework {
	case FrameworkNextJS, FrameworkTanStackStart:
		// Node.js based frameworks
		nodeVersion := DetectNodeVersion(projectDir)
		pkgManager := DetectPackageManager(projectDir)

		data.NodeVersion = nodeVersion
		data.PackageManager = string(pkgManager.Name)
		data.InstallCmd = pkgManager.InstallCmd
		data.BuildCmd = pkgManager.BuildCmd
		data.StartCmd = pkgManager.StartCmd
		data.LockFile = pkgManager.LockFile

	case FrameworkDjango:
		// Python based framework
		data.PythonVersion = DetectPythonVersion(projectDir)
		data.ProjectName = DetectDjangoProject(projectDir)
	}

	return data
}
