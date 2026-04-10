package config

import (
	"fmt"
	"path"
	"strings"
)

// VolumeSourceType describes whether a volume spec source is a named volume or a bind mount.
type VolumeSourceType string

const (
	VolumeSourceTypeNamed VolumeSourceType = "named"
	VolumeSourceTypeBind  VolumeSourceType = "bind"
)

// VolumeSpec is a parsed volume specification using Linux deployment semantics.
// Haloy deploys to Linux Docker daemons, so both bind mount sources and container
// targets are interpreted as Linux paths even when the CLI runs on another OS.
type VolumeSpec struct {
	Raw        string
	Source     string
	Target     string
	Options    string
	SourceType VolumeSourceType
}

func (vs VolumeSpec) IsNamedVolume() bool {
	return vs.SourceType == VolumeSourceTypeNamed
}

// ParseVolumeSpec validates and parses a Docker-style volume specification.
// Accepted format: source:/container/path[:options]
func ParseVolumeSpec(raw string) (VolumeSpec, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return VolumeSpec{}, fmt.Errorf("invalid volume mapping '%s'; expected 'host-path:/container/path[:options]'", raw)
	}

	if source, ok := windowsDriveSource(raw); ok {
		return VolumeSpec{}, fmt.Errorf(
			"volume host path '%s' in '%s' must use a Linux absolute path when using filesystem bind mounts. Windows-style paths are not supported because Haloy deploys to Linux servers",
			source,
			raw,
		)
	}

	parts := strings.SplitN(raw, ":", 3)
	if len(parts) < 2 {
		return VolumeSpec{}, fmt.Errorf("invalid volume mapping '%s'; expected 'host-path:/container/path[:options]'", raw)
	}

	source := strings.TrimSpace(parts[0])
	target := strings.TrimSpace(parts[1])
	options := ""
	if len(parts) == 3 {
		options = strings.TrimSpace(parts[2])
	}

	if source == "" {
		return VolumeSpec{}, fmt.Errorf("volume host path cannot be empty in '%s'", raw)
	}

	if strings.Contains(source, `\`) {
		return VolumeSpec{}, fmt.Errorf(
			"volume host path '%s' in '%s' must use a Linux absolute path when using filesystem bind mounts. Windows-style paths are not supported because Haloy deploys to Linux servers",
			source,
			raw,
		)
	}

	sourceType := VolumeSourceTypeNamed
	if path.IsAbs(source) {
		sourceType = VolumeSourceTypeBind
	} else if strings.HasPrefix(source, ".") || strings.Contains(source, "/") {
		return VolumeSpec{}, fmt.Errorf(
			"volume host path '%s' in '%s' must be absolute when using filesystem bind mounts. Relative paths don't work when the daemon runs in a container",
			source,
			raw,
		)
	}

	if !path.IsAbs(target) {
		return VolumeSpec{}, fmt.Errorf("volume container path '%s' in '%s' is not an absolute path", target, raw)
	}

	return VolumeSpec{
		Raw:        raw,
		Source:     source,
		Target:     target,
		Options:    options,
		SourceType: sourceType,
	}, nil
}

func windowsDriveSource(raw string) (string, bool) {
	if len(raw) < 4 {
		return "", false
	}

	if !isASCIIAlpha(raw[0]) || raw[1] != ':' || (raw[2] != '\\' && raw[2] != '/') {
		return "", false
	}

	nextColon := strings.Index(raw[3:], ":")
	if nextColon == -1 {
		return "", false
	}

	return raw[:3+nextColon], true
}

func isASCIIAlpha(b byte) bool {
	return ('a' <= b && b <= 'z') || ('A' <= b && b <= 'Z')
}
