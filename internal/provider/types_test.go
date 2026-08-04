package provider

import (
	"io"
	"testing"

	"github.com/uinaf/autoreview/internal/config"
	"github.com/uinaf/autoreview/internal/protocol"
)

func TestUnknownProviderFailsClosedWithoutMalformedStrictCredentialDiagnostic(t *testing.T) {
	t.Parallel()

	effective := config.Effective{
		Engine:    config.Value[protocol.ProviderName]{Value: protocol.ProviderName("future"), Source: config.SourceFlag},
		Isolation: config.Value[protocol.Isolation]{Value: protocol.IsolationStrict, Source: config.SourceFlag},
	}
	failure := strictCredentialFailure(effective, protocol.ProviderName("future"), nil)
	if failure == nil || failure.Class != protocol.FailureAuth || failure.Message == "" {
		t.Fatalf("failure = %#v", failure)
	}
	if recovery := strictCredentialRecovery(effective, protocol.ProviderName("future")); recovery == "" {
		t.Fatalf("recovery = %q", recovery)
	}
}

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
