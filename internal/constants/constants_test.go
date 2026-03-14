package constants

import (
	"runtime/debug"
	"testing"
)

func TestVersionFromBuildInfo(t *testing.T) {
	t.Run("uses module version when available", func(t *testing.T) {
		version := versionFromBuildInfo("v1.2.3", nil)
		if version != "v1.2.3" {
			t.Fatalf("expected module version, got %q", version)
		}
	})

	t.Run("falls back to dev when no vcs data exists", func(t *testing.T) {
		version := versionFromBuildInfo("(devel)", nil)
		if version != "dev" {
			t.Fatalf("expected dev fallback, got %q", version)
		}
	})

	t.Run("builds dev version from vcs revision", func(t *testing.T) {
		version := versionFromBuildInfo("(devel)", []debug.BuildSetting{
			{Key: "vcs.revision", Value: "0123456789abcdef"},
		})
		if version != "dev-0123456789ab" {
			t.Fatalf("expected truncated vcs version, got %q", version)
		}
	})

	t.Run("marks dirty working trees", func(t *testing.T) {
		version := versionFromBuildInfo("(devel)", []debug.BuildSetting{
			{Key: "vcs.revision", Value: "0123456789abcdef"},
			{Key: "vcs.modified", Value: "true"},
		})
		if version != "dev-0123456789ab-dirty" {
			t.Fatalf("expected dirty vcs version, got %q", version)
		}
	})
}
