package provider

import (
	"io"
	"testing"
)

func TestPromptReaderStreamsImmutableSegmentsAndCanBeReopened(t *testing.T) {
	request := Request{Prompt: "frozen-bundle", TrustedSuffix: "\nretry-instruction"}
	for attempt := range 2 {
		input, err := io.ReadAll(request.promptReader("\nprovider-protocol"))
		if err != nil {
			t.Fatal(err)
		}
		if got, want := string(input), "frozen-bundle\nretry-instruction\nprovider-protocol"; got != want {
			t.Fatalf("attempt %d input = %q, want %q", attempt+1, got, want)
		}
	}
	if request.Prompt != "frozen-bundle" || request.TrustedSuffix != "\nretry-instruction" {
		t.Fatal("reading provider input changed its immutable segments")
	}
}
