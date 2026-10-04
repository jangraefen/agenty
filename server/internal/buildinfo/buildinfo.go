// Package buildinfo holds values injected at build time via -ldflags.
package buildinfo

// Version is the release version, set with
// -ldflags "-X github.com/jangraefen/agenty/server/internal/buildinfo.Version=<version>".
var Version = "dev"
