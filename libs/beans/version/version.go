// Package version exposes build-time metadata for beans binaries.
//
// Until the binary embeds real ldflag values (planned for the cmd/ entrypoint
// task), Version reports "dev". Consumers should treat this as advisory only.
package version

// Version is the human-readable version string for the running binary. It is
// overridable at build time via:
//
//	-ldflags "-X github.com/mattsp1290/beans/version.Version=<value>"
var Version = "dev"
