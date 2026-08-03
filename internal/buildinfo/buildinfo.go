package buildinfo

import (
	"fmt"
	"runtime/debug"

	"golang.org/x/mod/module"
)

var (
	version = "dev"
	commit  = "unknown"
)

func Version() string {
	build, ok := debug.ReadBuildInfo()
	resolvedVersion, resolvedCommit := resolve(version, commit, build, ok)
	return fmt.Sprintf("autoreview %s (%s)", resolvedVersion, resolvedCommit)
}

func resolve(linkedVersion, linkedCommit string, build *debug.BuildInfo, ok bool) (string, string) {
	if !ok || build == nil {
		return linkedVersion, linkedCommit
	}
	if linkedVersion == "dev" && isReleaseModuleVersion(build.Main.Version) {
		linkedVersion = build.Main.Version
	}
	if linkedCommit == "unknown" {
		for _, setting := range build.Settings {
			if setting.Key == "vcs.revision" && setting.Value != "" {
				linkedCommit = setting.Value
				break
			}
		}
	}
	return linkedVersion, linkedCommit
}

func isReleaseModuleVersion(value string) bool {
	return value != "" && value != "(devel)" && !module.IsPseudoVersion(value)
}
