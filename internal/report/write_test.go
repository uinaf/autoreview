package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/uinaf/autoreview/internal/protocol"
)

func TestWriteTerminalEscapesFailureControls(t *testing.T) {
	t.Parallel()

	report := protocol.Report{
		SchemaVersion: protocol.SchemaVersion,
		Status:        protocol.StatusFailure,
		Failure:       &protocol.Failure{Class: protocol.FailureProvider, Message: "first\n\x1b[31msecond"},
		Metadata:      protocol.Metadata{Attempts: []protocol.Attempt{}},
	}
	var output bytes.Buffer
	if err := WriteTerminal(&output, report); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsRune(output.String(), '\x1b') || strings.Contains(output.String(), "first\n") || !strings.Contains(output.String(), `first\u000a`) || !strings.Contains(output.String(), `\u001b`) {
		t.Fatalf("unsafe terminal output: %q", output.String())
	}
}

func TestWriteJSONEmitsOneValidatedDocument(t *testing.T) {
	t.Parallel()

	report := protocol.Report{
		SchemaVersion: protocol.SchemaVersion,
		Status:        protocol.StatusFailure,
		Failure:       &protocol.Failure{Class: protocol.FailureConfig, Message: "invalid config"},
		Metadata:      protocol.Metadata{Attempts: []protocol.Attempt{}},
	}
	var output bytes.Buffer
	if err := WriteJSON(&output, report); err != nil {
		t.Fatal(err)
	}
	if bytes.Count(output.Bytes(), []byte("\n")) != 1 {
		t.Fatalf("JSON output is not one line: %q", output.String())
	}
	var decoded protocol.Report
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if err := decoded.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestWriteTerminalIndentsMultilineSummary(t *testing.T) {
	t.Parallel()

	isolation := protocol.IsolationStrict
	report := protocol.Report{
		SchemaVersion: protocol.SchemaVersion,
		Status:        protocol.StatusClean,
		Review: &protocol.Review{
			Findings:           []protocol.Finding{},
			OverallExplanation: "first line\nstatus: findings",
			OverallConfidence:  0.9,
		},
		Metadata: protocol.Metadata{
			Target: &protocol.Target{
				Mode:         protocol.TargetLocal,
				SnapshotHash: "sha256:test",
				HeadRevision: "abc",
				Files:        []protocol.ReviewedFile{{FilePath: "app.go", LineRanges: []protocol.LineRange{{StartLine: 1, EndLine: 1}}}},
			},
			Provider:  &protocol.Provider{Name: protocol.ProviderCodex, Model: "model", Version: "1.0.0"},
			Attempts:  []protocol.Attempt{{Number: 1, Outcome: protocol.AttemptValid}},
			Isolation: &isolation,
		},
	}
	var output bytes.Buffer
	if err := WriteTerminal(&output, report); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "\nstatus: findings\n") || !strings.Contains(output.String(), "\n   status: findings\n") {
		t.Fatalf("summary continuation was not indented: %q", output.String())
	}
}

func TestWritersRejectInvalidReport(t *testing.T) {
	t.Parallel()

	invalid := protocol.Report{SchemaVersion: protocol.SchemaVersion, Status: protocol.StatusClean}
	if err := WriteTerminal(&bytes.Buffer{}, invalid); err == nil {
		t.Fatal("WriteTerminal() accepted invalid report")
	}
	if err := WriteJSON(&bytes.Buffer{}, invalid); err == nil {
		t.Fatal("WriteJSON() accepted invalid report")
	}
}
