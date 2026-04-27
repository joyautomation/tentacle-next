package version

// These variables are set at build time via ldflags.
var (
	Version = "dev"
	Commit  = ""
	Date    = ""
)

// Role is the explicit identity of this binary at runtime: "tentacle" for a
// regular edge node, "mantle" for the central control plane build. Set by
// the build-tag-gated init() in role_mantle.go; defaults to "tentacle".
var Role = "tentacle"
