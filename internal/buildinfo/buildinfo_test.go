package buildinfo

import (
	"runtime/debug"
	"testing"
)

func TestResolveWithoutBuildInfo(t *testing.T) {
	gotVersion, gotCommit := resolve("dev", "unknown", nil, false)
	if gotVersion != "dev" || gotCommit != "unknown" {
		t.Fatalf("resolve = %q, %q", gotVersion, gotCommit)
	}
}

func TestResolveUsesModuleVersionAndRevisionForGoInstall(t *testing.T) {
	gotVersion, gotCommit := resolve("dev", "unknown", &debug.BuildInfo{
		Main: debug.Module{Version: "v0.1.0"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "0123456789abcdef"},
		},
	}, true)
	if gotVersion != "v0.1.0" || gotCommit != "0123456789abcdef" {
		t.Fatalf("resolve = %q, %q", gotVersion, gotCommit)
	}
}

func TestResolvePreservesReleaseLinkerOverrides(t *testing.T) {
	gotVersion, gotCommit := resolve("v0.1.0", "release-commit", &debug.BuildInfo{
		Main: debug.Module{Version: "(devel)"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "workspace-commit"},
		},
	}, true)
	if gotVersion != "v0.1.0" || gotCommit != "release-commit" {
		t.Fatalf("resolve = %q, %q", gotVersion, gotCommit)
	}
}

func TestResolveRejectsLocalPseudoVersion(t *testing.T) {
	gotVersion, gotCommit := resolve("dev", "unknown", &debug.BuildInfo{
		Main: debug.Module{Version: "v0.0.0-20260802235352-863e8871fda5+dirty"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "863e8871fda57ca5"},
		},
	}, true)
	if gotVersion != "dev" || gotCommit != "863e8871fda57ca5" {
		t.Fatalf("resolve = %q, %q", gotVersion, gotCommit)
	}
}
