// Package version exposes build-time metadata for the bn binary.
package version

// Version is the human-readable version string for the running binary. It is
// overridable at build time via:
//
//	-ldflags "-X github.com/mattsp1290/beans/version.Version=<value>"
var Version = "dev"
