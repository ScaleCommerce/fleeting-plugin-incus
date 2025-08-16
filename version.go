package fleetingincus

// Version information for the fleeting-plugin-incus
const (
	// Version is the current version of the plugin
	Version = "v0.2.3"

	// ProviderID is the unique identifier for this provider
	ProviderID = "incus"
)

var (
	// BuildInfo contains build-specific information (can be set via ldflags)
	BuildInfo = "dev"

	// BuildDate contains the build date (can be set via ldflags)
	BuildDate = "unknown"

	// GitCommit contains the git commit hash (can be set via ldflags)
	GitCommit = "unknown"
)
