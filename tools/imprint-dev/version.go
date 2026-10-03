package main

import (
	"fmt"
	"io"
	"runtime/debug"
)

// version is empty in a plain `go build`/`go install` in the checkout, which is
// how this repository builds imprint-dev (docs/setup.md). A build that wants a
// release number sets it: go build -ldflags "-X main.version=0.10.0".
var version = ""

// buildVersion is what the binary knows about its own build.
type buildVersion struct {
	Version   string // version, else the module version, else "devel"
	Module    string
	GoVersion string
	Revision  string // vcs.revision; empty under go run and go test, which stamp no VCS data
	Time      string
	Modified  bool
}

var readBuildInfo = debug.ReadBuildInfo

func readBuildVersion() buildVersion {
	bv := buildVersion{Version: version}
	info, ok := readBuildInfo()
	if ok {
		bv.Module = info.Main.Path
		bv.GoVersion = info.GoVersion
		if bv.Version == "" && info.Main.Version != "" && info.Main.Version != "(devel)" {
			bv.Version = info.Main.Version
		}
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				bv.Revision = s.Value
			case "vcs.time":
				bv.Time = s.Value
			case "vcs.modified":
				bv.Modified = s.Value == "true"
			}
		}
	}
	if bv.Version == "" {
		bv.Version = "devel"
	}
	return bv
}

// coreVersion is the short form that judge writes into every verdict and log
// line: version, then "+" and the first 7 characters of the commit, then
// ".dirty" if the tree had uncommitted changes. Without VCS data it is the
// version alone.
func coreVersion() string {
	bv := readBuildVersion()
	s := bv.Version
	if bv.Revision != "" {
		rev := bv.Revision
		if len(rev) > 7 {
			rev = rev[:7]
		}
		s += "+" + rev
		if bv.Modified {
			s += ".dirty"
		}
	}
	return s
}

// runVersion prints the version: the first line is the short form, the rest
// the build data it is made from.
func runVersion(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		fmt.Fprintf(stderr, "imprint-dev version: unexpected argument %q\n", args[0])
		return exitError
	}
	bv := readBuildVersion()
	fmt.Fprintf(stdout, "imprint-dev %s\n", coreVersion())
	if bv.Module != "" {
		fmt.Fprintf(stdout, "module %s\n", bv.Module)
	}
	if bv.GoVersion != "" {
		fmt.Fprintf(stdout, "go %s\n", bv.GoVersion)
	}
	if bv.Revision != "" {
		fmt.Fprintf(stdout, "vcs.revision %s\nvcs.time %s\nvcs.modified %t\n", bv.Revision, bv.Time, bv.Modified)
	} else {
		fmt.Fprintln(stdout, "vcs: none (go run, go test or a build without VCS data)")
	}
	return exitOK
}
