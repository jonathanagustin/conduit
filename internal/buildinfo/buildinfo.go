// Package buildinfo reports how the running binary was built.
//
// The values are package variables stamped at link time with -ldflags -X, by
// both Containerfiles. That is a silent coupling: -X names a symbol by its
// full import path, and if one of these is renamed or the package moves, the
// linker flag stops matching and every release binary reports the development
// defaults instead. buildinfo_test.go reads the Containerfiles and checks the
// names still line up.
package buildinfo

// These values are overridden by release builds using -ldflags. Development
// binaries retain explicit, recognizable defaults.
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"buildDate"`
}

func Current() Info {
	return Info{Version: Version, Commit: Commit, BuildDate: BuildDate}
}
