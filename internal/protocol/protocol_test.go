package protocol

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodeReportFixtures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		file   string
		status Status
	}{
		{name: "clean", file: "report-clean.json", status: StatusClean},
		{name: "findings", file: "report-findings.json", status: StatusFindings},
		{name: "failure", file: "report-failure.json", status: StatusFailure},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(filepath.Join("testdata", test.file))
			if err != nil {
				t.Fatal(err)
			}
			report, err := DecodeReport(data)
			if err != nil {
				t.Fatalf("DecodeReport() error = %v", err)
			}
			if report.Status != test.status {
				t.Fatalf("status = %q, want %q", report.Status, test.status)
			}
		})
	}
}

func TestDecodeReportRejectsInvalidContracts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(map[string]any)
		wantErr string
	}{
		{
			name: "unknown root field",
			mutate: func(report map[string]any) {
				report["surprise"] = true
			},
			wantErr: `report has unknown field "surprise"`,
		},
		{
			name: "missing review field",
			mutate: func(report map[string]any) {
				delete(reviewMap(report), "overall_confidence")
			},
			wantErr: `report.review missing required field "overall_confidence"`,
		},
		{
			name: "invalid priority",
			mutate: func(report map[string]any) {
				asFindings(report)[0]["priority"] = "P4"
			},
			wantErr: `invalid priority "P4"`,
		},
		{
			name: "confidence above one",
			mutate: func(report map[string]any) {
				asFindings(report)[0]["confidence"] = 1.1
			},
			wantErr: "confidence must be between 0 and 1",
		},
		{
			name: "null confidence",
			mutate: func(report map[string]any) {
				asFindings(report)[0]["confidence"] = nil
			},
			wantErr: "report.review.findings[0].confidence must not be null",
		},
		{
			name: "quoted line number",
			mutate: func(report map[string]any) {
				asFindings(report)[0]["location"].(map[string]any)["start_line"] = "12"
			},
			wantErr: "must be a JSON number",
		},
		{
			name: "absolute location",
			mutate: func(report map[string]any) {
				asFindings(report)[0]["location"].(map[string]any)["file_path"] = "/tmp/main.go"
			},
			wantErr: "must be a normalized repository-relative POSIX path",
		},
		{
			name: "whitespace-only location",
			mutate: func(report map[string]any) {
				asFindings(report)[0]["location"].(map[string]any)["file_path"] = "   "
			},
			wantErr: "must be a normalized repository-relative POSIX path",
		},
		{
			name: "unreviewed file",
			mutate: func(report map[string]any) {
				asFindings(report)[0]["location"].(map[string]any)["file_path"] = "other.go"
			},
			wantErr: `references unreviewed file "other.go"`,
		},
		{
			name: "unreviewed lines",
			mutate: func(report map[string]any) {
				asFindings(report)[0]["location"].(map[string]any)["start_line"] = float64(30)
				asFindings(report)[0]["location"].(map[string]any)["end_line"] = float64(31)
			},
			wantErr: "references unreviewed lines",
		},
		{
			name: "line number above protocol maximum",
			mutate: func(report map[string]any) {
				asFindings(report)[0]["location"].(map[string]any)["end_line"] = float64(MaxLineNumber + 1)
			},
			wantErr: "line numbers must not exceed",
		},
		{
			name: "clean with findings",
			mutate: func(report map[string]any) {
				report["status"] = "clean"
			},
			wantErr: "clean status requires zero findings",
		},
		{
			name: "invalid provider",
			mutate: func(report map[string]any) {
				metadataMap(report)["provider"].(map[string]any)["name"] = "oracle"
			},
			wantErr: `invalid provider.name "oracle"`,
		},
		{
			name: "non-sequential attempt",
			mutate: func(report map[string]any) {
				metadataMap(report)["attempts"].([]any)[0].(map[string]any)["number"] = float64(2)
			},
			wantErr: "attempt 1 has non-sequential number 2",
		},
		{
			name: "strategy without recovery",
			mutate: func(report map[string]any) {
				strategy := "cursor_trailing_object"
				metadataMap(report)["protocol_recovery"].(map[string]any)["strategy"] = strategy
			},
			wantErr: "strategy requires applied=true",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			report := findingsFixture(t)
			test.mutate(report)
			data, err := json.Marshal(report)
			if err != nil {
				t.Fatal(err)
			}
			_, err = DecodeReport(data)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("DecodeReport() error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func TestDecodeReportRejectsMultipleJSONValues(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("testdata", "report-clean.json"))
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, []byte("\n{}")...)
	if _, err := DecodeReport(data); err == nil {
		t.Fatal("DecodeReport() unexpectedly accepted multiple JSON values")
	}
}

func TestDecodeRejectsInvalidUTF8(t *testing.T) {
	t.Parallel()

	reportData, err := os.ReadFile(filepath.Join("testdata", "report-clean.json"))
	if err != nil {
		t.Fatal(err)
	}
	reportData = bytes.Replace(reportData, []byte("No actionable defects found."), []byte{'b', 'a', 'd', 0xff}, 1)
	if _, err := DecodeReport(reportData); err == nil || !strings.Contains(err.Error(), "invalid UTF-8") {
		t.Fatalf("DecodeReport() error = %v, want invalid UTF-8 error", err)
	}

	reviewData := []byte("{\"findings\":[],\"overall_explanation\":\"bad\xff\",\"overall_confidence\":1}")
	if _, err := DecodeReview(reviewData); err == nil || !strings.Contains(err.Error(), "invalid UTF-8") {
		t.Fatalf("DecodeReview() error = %v, want invalid UTF-8 error", err)
	}
}

func TestDecodeReportAcceptsExactIntegerJSONNumbers(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("testdata", "report-findings.json"))
	if err != nil {
		t.Fatal(err)
	}
	replacements := [][2]string{
		{`"start_line": 12`, `"start_line": 12.0`},
		{`"start_line": 10`, `"start_line": 1e1`},
		{`"number": 1`, `"number": 1.0`},
		{`"duration_ms": 2100`, `"duration_ms": 2.1e3`},
		{`"duration_ms": 2200`, `"duration_ms": 22e2`},
	}
	for _, replacement := range replacements {
		data = bytes.Replace(data, []byte(replacement[0]), []byte(replacement[1]), 1)
	}
	if _, err := DecodeReport(data); err != nil {
		t.Fatalf("DecodeReport() rejected exact integer representations: %v", err)
	}

	data = bytes.Replace(data, []byte(`"end_line": 14`), []byte(`"end_line": 14.5`), 1)
	if _, err := DecodeReport(data); err == nil || !strings.Contains(err.Error(), "exactly representable integer") {
		t.Fatalf("DecodeReport() error = %v, want non-integer error", err)
	}
}

func TestExactInt64Bounds(t *testing.T) {
	t.Parallel()

	valid := map[string]int64{
		"9223372036854775807":  9223372036854775807,
		"-9223372036854775808": -9223372036854775808,
		"12.0":                 12,
		"1.2e1":                12,
		"1200e-2":              12,
		"0e999999999":          0,
	}
	for input, want := range valid {
		value, err := exactInt64([]byte(input))
		if err != nil || value != want {
			t.Errorf("exactInt64(%s) = %d, %v; want %d", input, value, err, want)
		}
	}
	for _, input := range []string{"9223372036854775808", "-9223372036854775809", "12.5", "1e999999999", "1e-999999999"} {
		if _, err := exactInt64([]byte(input)); err == nil {
			t.Errorf("exactInt64(%s) unexpectedly succeeded", input)
		}
	}
}

func TestDecodeReportRejectsDuplicateFields(t *testing.T) {
	t.Parallel()

	data := []byte(`{
		"schema_version": "1",
		"schema_version": "1",
		"status": "failure",
		"review": null,
		"failure": {"class": "config", "message": "invalid"},
		"metadata": {
			"target": null,
			"provider": null,
			"attempts": [],
			"duration_ms": 0,
			"isolation": null,
			"web_access": false,
			"protocol_recovery": {"applied": false, "strategy": null}
		}
	}`)
	if _, err := DecodeReport(data); err == nil || !strings.Contains(err.Error(), `duplicate field "schema_version"`) {
		t.Fatalf("DecodeReport() error = %v, want duplicate-field error", err)
	}
}

func TestDecodeReview(t *testing.T) {
	t.Parallel()

	valid := []byte(`{
		"findings": [],
		"overall_explanation": "No actionable defects found.",
		"overall_confidence": 0.9
	}`)
	review, err := DecodeReview(valid)
	if err != nil {
		t.Fatalf("DecodeReview() error = %v", err)
	}
	if len(review.Findings) != 0 {
		t.Fatalf("findings = %d, want 0", len(review.Findings))
	}

	invalid := []byte(`{
		"findings": [],
		"overall_explanation": "No actionable defects found.",
		"overall_confidence": 0.9,
		"verdict": "clean"
	}`)
	if _, err := DecodeReview(invalid); err == nil || !strings.Contains(err.Error(), `unknown field "verdict"`) {
		t.Fatalf("DecodeReview() error = %v, want unknown-field error", err)
	}
}

func TestReportValidateSemanticRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*Report)
		wantErr string
	}{
		{
			name: "nil findings",
			mutate: func(report *Report) {
				report.Review.Findings = nil
			},
			wantErr: "findings must be an array",
		},
		{
			name: "nil attempts",
			mutate: func(report *Report) {
				report.Metadata.Attempts = nil
			},
			wantErr: "metadata.attempts must be an array",
		},
		{
			name: "unsupported schema",
			mutate: func(report *Report) {
				report.SchemaVersion = "2"
			},
			wantErr: "unsupported schema_version",
		},
		{
			name: "missing provider on valid result",
			mutate: func(report *Report) {
				report.Metadata.Provider = nil
			},
			wantErr: "requires target, provider, and isolation metadata",
		},
		{
			name: "invalid isolation",
			mutate: func(report *Report) {
				value := Isolation("loose")
				report.Metadata.Isolation = &value
			},
			wantErr: "invalid metadata.isolation",
		},
		{
			name: "negative duration",
			mutate: func(report *Report) {
				report.Metadata.DurationMS = -1
			},
			wantErr: "metadata.duration_ms must be non-negative",
		},
		{
			name: "local target with base",
			mutate: func(report *Report) {
				report.Metadata.Target.Mode = TargetLocal
				report.Metadata.Target.BaseRevision = "origin/main"
				report.Metadata.Target.CommitRevision = ""
			},
			wantErr: "local target forbids base_revision",
		},
		{
			name: "commit target without commit",
			mutate: func(report *Report) {
				report.Metadata.Target.Mode = TargetCommit
				report.Metadata.Target.CommitRevision = ""
			},
			wantErr: "commit target requires commit_revision",
		},
		{
			name: "duplicate reviewed file",
			mutate: func(report *Report) {
				report.Metadata.Target.Files = append(report.Metadata.Target.Files, report.Metadata.Target.Files[0])
			},
			wantErr: "duplicate path",
		},
		{
			name: "overlapping reviewed ranges",
			mutate: func(report *Report) {
				report.Metadata.Target.Files[0].LineRanges = append(report.Metadata.Target.Files[0].LineRanges, LineRange{StartLine: 15, EndLine: 25})
			},
			wantErr: "overlapping or unsorted line ranges",
		},
		{
			name: "blank provider model",
			mutate: func(report *Report) {
				report.Metadata.Provider.Model = "   "
			},
			wantErr: "provider.model must be non-empty",
		},
		{
			name: "valid attempt with error class",
			mutate: func(report *Report) {
				value := FailureProtocol
				report.Metadata.Attempts[0].ErrorClass = &value
			},
			wantErr: "forbids error_class",
		},
		{
			name: "recovery without strategy",
			mutate: func(report *Report) {
				report.Metadata.ProtocolRecovery.Applied = true
			},
			wantErr: "invalid strategy",
		},
		{
			name: "cursor recovery with non-cursor provider",
			mutate: func(report *Report) {
				strategy := RecoveryCursorTrailingObject
				report.Metadata.ProtocolRecovery = ProtocolRecovery{Applied: true, Strategy: &strategy}
			},
			wantErr: "requires provider cursor",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			report := decodedFindingsFixture(t)
			test.mutate(&report)
			err := report.Validate()
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("Validate() error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func TestReviewValidateCountsUnicodeCharacters(t *testing.T) {
	t.Parallel()

	review := Review{
		Findings:           []Finding{},
		OverallExplanation: strings.Repeat("é", 3000),
		OverallConfidence:  1,
	}
	if err := review.Validate(); err != nil {
		t.Fatalf("Validate() rejected 3000 Unicode characters: %v", err)
	}
	review.OverallExplanation += "é"
	if err := review.Validate(); err == nil || !strings.Contains(err.Error(), "3000 characters") {
		t.Fatalf("Validate() error = %v, want character limit error", err)
	}
}

func TestValidatedFixturesMarshalRoundTrip(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"report-clean.json", "report-findings.json", "report-failure.json"} {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(filepath.Join("testdata", name))
			if err != nil {
				t.Fatal(err)
			}
			report, err := DecodeReport(data)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(report)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := DecodeReport(encoded); err != nil {
				t.Fatalf("validated report did not round-trip: %v", err)
			}
		})
	}
}

func findingsFixture(t *testing.T) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "report-findings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	return report
}

func decodedFindingsFixture(t *testing.T) Report {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "report-findings.json"))
	if err != nil {
		t.Fatal(err)
	}
	report, err := DecodeReport(data)
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func reviewMap(report map[string]any) map[string]any {
	return report["review"].(map[string]any)
}

func metadataMap(report map[string]any) map[string]any {
	return report["metadata"].(map[string]any)
}

func asFindings(report map[string]any) []map[string]any {
	items := reviewMap(report)["findings"].([]any)
	findings := make([]map[string]any, len(items))
	for index, item := range items {
		findings[index] = item.(map[string]any)
	}
	return findings
}
