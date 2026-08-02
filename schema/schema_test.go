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
		name := name
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

func TestSchemasRejectWhitespaceOnlyPaths(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("..", "internal", "protocol", "testdata", "report-findings.json"))
	if err != nil {
		t.Fatal(err)
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	report := instance.(map[string]any)
	review := report["review"].(map[string]any)
	finding := review["findings"].([]any)[0].(map[string]any)
	finding["location"].(map[string]any)["file_path"] = "   "
	if err := compileSchema(t, "review-v1.schema.json").Validate(review); err == nil {
		t.Fatal("review schema accepted a whitespace-only finding path")
	}

	instance, err = jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	report = instance.(map[string]any)
	metadata := report["metadata"].(map[string]any)
	target := metadata["target"].(map[string]any)
	reviewedFile := target["files"].([]any)[0].(map[string]any)
	reviewedFile["file_path"] = "   "
	if err := compileSchema(t, "result-v1.schema.json").Validate(report); err == nil {
		t.Fatal("result schema accepted a whitespace-only reviewed file path")
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
