package schema_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
