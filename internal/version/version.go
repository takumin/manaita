package version

var (
	version    string = "unknown"
	revision   string = "unknown"
	prerelease string
)

func Version() string {
	return version
}

func Revision() string {
	return revision
}

// Prerelease returns the prerelease suffix of the build, such as "dev",
// or an empty string for a build from an exact release tag.
func Prerelease() string {
	return prerelease
}
