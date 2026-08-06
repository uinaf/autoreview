package provider

import (
	"slices"
	"testing"
)

func TestContainsCapabilityMatchesExactTokens(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		output string
		want   bool
	}{
		{name: "exact", output: "Usage: --sandbox <profile>", want: true},
		{name: "punctuation boundary", output: "[--sandbox]", want: true},
		{name: "suffix collision", output: "--sandbox-legacy", want: false},
		{name: "prefix collision", output: "legacy--sandbox", want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := containsCapability(test.output, "--sandbox"); got != test.want {
				t.Fatalf("containsCapability(%q) = %t, want %t", test.output, got, test.want)
			}
		})
	}
}

func TestMissingCapabilitiesRejectsTokenCollisions(t *testing.T) {
	t.Parallel()

	got := missingCapabilities("--prompt-file-old legacy--sandbox --model", []string{"--prompt-file", "--sandbox", "--model"})
	want := []string{"--prompt-file", "--sandbox"}
	if !slices.Equal(got, want) {
		t.Fatalf("missingCapabilities() = %v, want %v", got, want)
	}
}

func TestOptionSupportsMatchesExactOptionAndValueTokens(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		help string
		want bool
	}{
		{name: "exact", help: "--output-format <format>\n  possible values: plain, json", want: true},
		{name: "option suffix collision", help: "--output-format-old <format>\n  possible values: json", want: false},
		{name: "option prefix collision", help: "legacy--output-format <format>\n  possible values: json", want: false},
		{name: "value suffix collision", help: "--output-format <format>\n  possible values: json-lines", want: false},
		{name: "value prefix collision", help: "--output-format <format>\n  possible values: notjson", want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := optionSupports(test.help, "--output-format", "json"); got != test.want {
				t.Fatalf("optionSupports() = %t, want %t for %q", got, test.want, test.help)
			}
		})
	}
}
