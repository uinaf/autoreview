package schema_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestExternalReferencesResolveBesideSchema(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"review-v1.schema.json", "result-v1.schema.json"} {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		var document any
		if err := json.Unmarshal(data, &document); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, reference := range references(document) {
			if strings.HasPrefix(reference, "#") || strings.Contains(reference, "://") {
				continue
			}
			target := strings.SplitN(reference, "#", 2)[0]
			if _, err := os.Stat(filepath.Join(filepath.Dir(name), target)); err != nil {
				t.Errorf("%s reference %q does not resolve: %v", name, reference, err)
			}
		}
	}
}

func TestFixturesValidateAgainstSchemas(t *testing.T) {
	t.Parallel()

	resultSchema := compileSchema(t, "result-v1.schema.json")
	reviewSchema := compileSchema(t, "review-v1.schema.json")
	for _, name := range []string{"report-clean.json", "report-findings.json", "report-failure.json"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(filepath.Join("..", "internal", "protocol", "testdata", name))
			if err != nil {
				t.Fatal(err)
			}
			instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			if err := resultSchema.Validate(instance); err != nil {
				t.Fatalf("result schema rejected fixture: %v", err)
			}
			report := instance.(map[string]any)
			if report["review"] != nil {
				if err := reviewSchema.Validate(report["review"]); err != nil {
					t.Fatalf("review schema rejected fixture review: %v", err)
				}
			}
		})
	}
}

func TestSchemasRejectUnsafePaths(t *testing.T) {
	t.Parallel()

	reviewSchema := compileSchema(t, "review-v1.schema.json")
	resultSchema := compileSchema(t, "result-v1.schema.json")
	paths := []string{"   ", "/etc/passwd", "../../secret", "C:main.go", "C:/main.go", `a\b`, "a/../b", "a/./b", "a//b", "a/", "a/\x01b", "a/\u0085b"}
	for _, invalidPath := range paths {
		invalidPath := invalidPath
		t.Run(invalidPath, func(t *testing.T) {
			t.Parallel()
			report := findingsInstance(t)
			review := report["review"].(map[string]any)
			finding := review["findings"].([]any)[0].(map[string]any)
			finding["location"].(map[string]any)["file_path"] = invalidPath
			if err := reviewSchema.Validate(review); err == nil {
				t.Errorf("review schema accepted unsafe path %q", invalidPath)
			}

			report = findingsInstance(t)
			metadata := report["metadata"].(map[string]any)
			target := metadata["target"].(map[string]any)
			reviewedFile := target["files"].([]any)[0].(map[string]any)
			reviewedFile["file_path"] = invalidPath
			if err := resultSchema.Validate(report); err == nil {
				t.Errorf("result schema accepted unsafe path %q", invalidPath)
			}
		})
	}
}

func TestResultSchemaRejectsIncompleteSuccessMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "null target", mutate: func(metadata map[string]any) { metadata["target"] = nil }},
		{name: "null provider", mutate: func(metadata map[string]any) { metadata["provider"] = nil }},
		{name: "null isolation", mutate: func(metadata map[string]any) { metadata["isolation"] = nil }},
		{name: "empty attempts", mutate: func(metadata map[string]any) { metadata["attempts"] = []any{} }},
	}
	schema := compileSchema(t, "result-v1.schema.json")
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			report := findingsInstance(t)
			test.mutate(report["metadata"].(map[string]any))
			if err := schema.Validate(report); err == nil {
				t.Fatal("result schema accepted incomplete success metadata")
			}
		})
	}
}

func TestResultSchemaRejectsAmbiguousTargetIdentity(t *testing.T) {
	t.Parallel()

	schema := compileSchema(t, "result-v1.schema.json")
	report := findingsInstance(t)
	target := report["metadata"].(map[string]any)["target"].(map[string]any)
	target["mode"] = "local"
	target["head_revision"] = ""
	target["base_revision"] = ""
	if err := schema.Validate(report); err == nil {
		t.Fatal("result schema accepted a local target without head_revision")
	}

	report = findingsInstance(t)
	target = report["metadata"].(map[string]any)["target"].(map[string]any)
	target["mode"] = "commit"
	target["commit_revision"] = "abcdef"
	if err := schema.Validate(report); err == nil {
		t.Fatal("result schema accepted a commit target with branch revisions")
	}
}

func compileSchema(t *testing.T, name string) *jsonschema.Schema {
	t.Helper()
	schema, err := jsonschema.NewCompiler().Compile(name)
	if err != nil {
		t.Fatalf("compile %s: %v", name, err)
	}
	return schema
}

func findingsInstance(t *testing.T) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "internal", "protocol", "testdata", "report-findings.json"))
	if err != nil {
		t.Fatal(err)
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return instance.(map[string]any)
}

func references(value any) []string {
	var found []string
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			if key == "$ref" {
				if reference, ok := child.(string); ok {
					found = append(found, reference)
				}
				continue
			}
			found = append(found, references(child)...)
		}
	case []any:
		for _, child := range value {
			found = append(found, references(child)...)
		}
	}
	return found
}
