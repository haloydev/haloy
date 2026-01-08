package init

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateDockerfile_NextJS(t *testing.T) {
	data := DockerfileData{
		NodeVersion:    "20",
		PackageManager: "pnpm",
		InstallCmd:     "pnpm install --frozen-lockfile",
		BuildCmd:       "pnpm run build",
		StartCmd:       "pnpm start",
		LockFile:       "pnpm-lock.yaml",
		Port:           "3000",
	}

	result, err := GenerateDockerfile(FrameworkNextJS, data)
	if err != nil {
		t.Fatalf("GenerateDockerfile() error = %v", err)
	}

	dockerfile := string(result)

	// Check key content
	checks := []struct {
		name     string
		contains string
	}{
		{"base image with correct node version", "FROM node:20-alpine AS base"},
		{"pnpm setup", "ENV PNPM_HOME"},
		{"corepack enable for pnpm", "corepack enable"},
		{"correct lockfile copy", "COPY package.json pnpm-lock.yaml"},
		{"pnpm install command", "pnpm install --frozen-lockfile"},
		{"pnpm build command", "pnpm run build"},
		{"telemetry disabled", "ENV NEXT_TELEMETRY_DISABLED=1"},
		{"port env var", "ENV PORT=3000"},
		{"expose port", "EXPOSE ${PORT}"},
		{"standalone output copy", ".next/standalone"},
		{"static files copy", ".next/static"},
		{"node server.js cmd", `CMD ["node", "server.js"]`},
		{"healthcheck commented out", "# HEALTHCHECK"},
	}

	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if !strings.Contains(dockerfile, check.contains) {
				t.Errorf("Dockerfile should contain %q", check.contains)
			}
		})
	}

	// Should NOT contain npm ci when using pnpm (npm install -g bun is ok for bun setup)
	if strings.Contains(dockerfile, "npm ci") {
		t.Error("Dockerfile should not contain 'npm ci' when using pnpm")
	}
}

func TestGenerateDockerfile_NextJS_NPM(t *testing.T) {
	data := DockerfileData{
		NodeVersion:    "22",
		PackageManager: "npm",
		InstallCmd:     "npm ci",
		BuildCmd:       "npm run build",
		StartCmd:       "npm start",
		LockFile:       "package-lock.json",
		Port:           "3000",
	}

	result, err := GenerateDockerfile(FrameworkNextJS, data)
	if err != nil {
		t.Fatalf("GenerateDockerfile() error = %v", err)
	}

	dockerfile := string(result)

	// Check npm-specific content
	if !strings.Contains(dockerfile, "FROM node:22-alpine") {
		t.Error("Should use node 22")
	}
	if !strings.Contains(dockerfile, "npm ci") {
		t.Error("Should use npm ci for install")
	}
	if !strings.Contains(dockerfile, "package-lock.json") {
		t.Error("Should copy package-lock.json")
	}

	// Should NOT contain pnpm setup
	if strings.Contains(dockerfile, "PNPM_HOME") {
		t.Error("Should not contain PNPM_HOME when using npm")
	}
}

func TestGenerateDockerfile_TanStackStart_PNPM(t *testing.T) {
	data := DockerfileData{
		NodeVersion:    "22",
		PackageManager: "pnpm",
		InstallCmd:     "pnpm install --frozen-lockfile",
		BuildCmd:       "pnpm run build",
		StartCmd:       "pnpm start",
		LockFile:       "pnpm-lock.yaml",
		Port:           "3000",
	}

	result, err := GenerateDockerfile(FrameworkTanStackStart, data)
	if err != nil {
		t.Fatalf("GenerateDockerfile() error = %v", err)
	}

	dockerfile := string(result)

	checks := []struct {
		name     string
		contains string
	}{
		{"slim base image", "FROM node:22-slim AS base"},
		{"pnpm setup", "ENV PNPM_HOME"},
		{"corepack enable", "corepack enable"},
		{"prod deps stage", "FROM base AS prod-deps"},
		{"build stage", "FROM base AS build"},
		{"output directory copy", ".output"},
		{"port env var", "ENV PORT=3000"},
		{"pnpm start cmd", `CMD ["pnpm", "start"]`},
	}

	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if !strings.Contains(dockerfile, check.contains) {
				t.Errorf("Dockerfile should contain %q", check.contains)
			}
		})
	}
}

func TestGenerateDockerfile_TanStackStart_Bun(t *testing.T) {
	data := DockerfileData{
		NodeVersion:    "22", // ignored for bun
		PackageManager: "bun",
		InstallCmd:     "bun install --frozen-lockfile",
		BuildCmd:       "bun run build",
		StartCmd:       "bun start",
		LockFile:       "bun.lockb",
		Port:           "3000",
	}

	result, err := GenerateDockerfile(FrameworkTanStackStart, data)
	if err != nil {
		t.Fatalf("GenerateDockerfile() error = %v", err)
	}

	dockerfile := string(result)

	// Check bun-specific content
	if !strings.Contains(dockerfile, "FROM oven/bun:1 AS base") {
		t.Error("Should use oven/bun base image")
	}
	if !strings.Contains(dockerfile, "bun install") {
		t.Error("Should use bun install")
	}
	if !strings.Contains(dockerfile, `CMD ["bun", "start"]`) {
		t.Error("Should use bun start command")
	}

	// Should NOT contain node-specific setup
	if strings.Contains(dockerfile, "corepack") {
		t.Error("Should not contain corepack when using bun")
	}
}

func TestGenerateDockerfile_Django(t *testing.T) {
	data := DockerfileData{
		PythonVersion: "3.12",
		ProjectName:   "myproject",
		Port:          "8000",
	}

	result, err := GenerateDockerfile(FrameworkDjango, data)
	if err != nil {
		t.Fatalf("GenerateDockerfile() error = %v", err)
	}

	dockerfile := string(result)

	checks := []struct {
		name     string
		contains string
	}{
		{"python base image", "FROM python:3.12-slim"},
		{"requirements copy", "COPY requirements.txt"},
		{"pip install", "pip install --no-cache-dir -r requirements.txt"},
		{"gunicorn install", "pip install --no-cache-dir gunicorn"},
		{"collectstatic", "python manage.py collectstatic --noinput"},
		{"non-root user", "adduser --system"},
		{"port env var", "ENV PORT=8000"},
		{"django project env", "ENV DJANGO_PROJECT=myproject"},
		{"gunicorn cmd", "gunicorn --bind 0.0.0.0:${PORT} ${DJANGO_PROJECT}.wsgi:application"},
	}

	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if !strings.Contains(dockerfile, check.contains) {
				t.Errorf("Dockerfile should contain %q", check.contains)
			}
		})
	}
}

func TestGenerateDockerfile_UnknownFramework(t *testing.T) {
	data := DockerfileData{}

	_, err := GenerateDockerfile(FrameworkUnknown, data)
	if err == nil {
		t.Error("GenerateDockerfile() should error for unknown framework")
	}
	if !strings.Contains(err.Error(), "no template available") {
		t.Errorf("Error should mention no template available, got: %v", err)
	}
}

func TestBuildDockerfileData_NextJS(t *testing.T) {
	testdataPath := getTestdataPath(t)
	projectDir := filepath.Join(testdataPath, "nextjs-pnpm")

	data := BuildDockerfileData(FrameworkNextJS, projectDir)

	if data.NodeVersion != "20" {
		t.Errorf("NodeVersion = %v, want 20", data.NodeVersion)
	}
	if data.PackageManager != "pnpm" {
		t.Errorf("PackageManager = %v, want pnpm", data.PackageManager)
	}
	if data.LockFile != "pnpm-lock.yaml" {
		t.Errorf("LockFile = %v, want pnpm-lock.yaml", data.LockFile)
	}
	if data.Port != "3000" {
		t.Errorf("Port = %v, want 3000", data.Port)
	}
}

func TestBuildDockerfileData_Django(t *testing.T) {
	testdataPath := getTestdataPath(t)
	projectDir := filepath.Join(testdataPath, "django-basic")

	data := BuildDockerfileData(FrameworkDjango, projectDir)

	if data.PythonVersion != "3.12" {
		t.Errorf("PythonVersion = %v, want 3.12", data.PythonVersion)
	}
	if data.ProjectName != "myproject" {
		t.Errorf("ProjectName = %v, want myproject", data.ProjectName)
	}
	if data.Port != "8000" {
		t.Errorf("Port = %v, want 8000", data.Port)
	}
}
