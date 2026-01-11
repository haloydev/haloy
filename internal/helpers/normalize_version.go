package helpers

// NormalizeVersion strips the 'v' prefix from version strings for comparison
func NormalizeVersion(version string) string {
	if len(version) > 0 && version[0] == 'v' {
		return version[1:]
	}
	return version
}
