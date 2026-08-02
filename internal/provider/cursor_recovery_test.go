package provider

import (
	"strings"
	"testing"
)

func TestDecodeCursorReviewRecoveryMatrix(t *testing.T) {
	t.Parallel()

	valid := `{"findings":[],"overall_explanation":"No defects.","overall_confidence":0.95}`
	invalid := `{"findings":[]}`
	tests := []struct {
		name      string
		input     string
		wantValid bool
		recovered bool
	}{
		{name: "exact object", input: valid, wantValid: true},
		{name: "exact object whitespace", input: " \n\t" + valid + "\n", wantValid: true},
		{name: "prose and trailing object", input: "Here is the requested review.\n" + valid, wantValid: true, recovered: true},
		{name: "multiline prose and trailing object", input: "Review complete.\nNo extra context follows.\n" + valid, wantValid: true, recovered: true},
		{name: "no object", input: "No findings."},
		{name: "invalid exact object", input: invalid},
		{name: "invalid recovered object", input: "Review follows.\n" + invalid},
		{name: "fenced object", input: "Review follows.\n```json\n" + valid + "\n```"},
		{name: "suffix prose", input: "Review follows.\n" + valid + "\nDone."},
		{name: "two objects", input: "Review follows.\n" + valid + "\n" + valid},
		{name: "opening brace in prose", input: "Use {braces} carefully.\n" + valid},
		{name: "closing brace in prose", input: "Unexpected } marker.\n" + valid},
		{name: "malformed object before candidate", input: "Review {oops\n" + valid},
		{name: "JSON scalar prefix", input: "42\n" + valid},
		{name: "JSON scalar then prose prefix", input: "42\nReview follows.\n" + valid},
		{name: "JSON null then prose prefix", input: "null\nReview follows.\n" + valid},
		{name: "JSON string prefix", input: `"preface"` + "\n" + valid},
		{name: "JSON string then prose prefix", input: `"preface"` + "\nReview follows.\n" + valid},
		{name: "array prefix and object", input: "[]\n" + valid},
		{name: "array then prose prefix", input: "[]\nReview follows.\n" + valid},
		{name: "multiple JSON values after prose", input: "Review follows.\n" + valid + "\nnull"},
		{name: "duplicate review key", input: "Review follows.\n" + strings.Replace(valid, `"findings":[]`, `"findings":[],"findings":[]`, 1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			review, recovered, err := decodeCursorReview(test.input)
			if test.wantValid {
				if err != nil {
					t.Fatal(err)
				}
				if recovered != test.recovered || review.OverallExplanation != "No defects." {
					t.Fatalf("review = %+v, recovered = %t", review, recovered)
				}
				return
			}
			if err == nil {
				t.Fatalf("decodeCursorReview() accepted input with recovery=%t: %+v", recovered, review)
			}
		})
	}
}
