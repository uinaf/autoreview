package protocol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSchemaRequiredFieldsMatchDecoderShapes(t *testing.T) {
	t.Parallel()

	result := schemaDocument(t, "result-v1.schema.json")
	review := schemaDocument(t, "review-v1.schema.json")
	assertSameFields(t, "report", requiredFields(t, result), reportShapeFields[:])
	assertSameFields(t, "review", requiredFields(t, review), reviewShapeFields[:])
	definitions := result["$defs"].(map[string]any)
	metadata := definitions["metadata"].(map[string]any)
	assertSameFields(t, "metadata", requiredFields(t, metadata), metadataShapeFields[:])
}

func schemaDocument(t *testing.T, name string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "schema", name))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	return document
}

func requiredFields(t *testing.T, schema map[string]any) []string {
	t.Helper()
	values, ok := schema["required"].([]any)
	if !ok {
		t.Fatal("schema has no required array")
	}
	fields := make([]string, len(values))
	for index, value := range values {
		field, ok := value.(string)
		if !ok {
			t.Fatalf("required[%d] is not a string", index)
		}
		fields[index] = field
	}
	return fields
}

func assertSameFields(t *testing.T, name string, schemaFields, decoderFields []string) {
	t.Helper()
	schemaSet := make(map[string]struct{}, len(schemaFields))
	for _, field := range schemaFields {
		schemaSet[field] = struct{}{}
	}
	decoderSet := make(map[string]struct{}, len(decoderFields))
	for _, field := range decoderFields {
		decoderSet[field] = struct{}{}
	}
	if len(schemaSet) != len(decoderSet) {
		t.Fatalf("%s required fields differ: schema=%v decoder=%v", name, schemaFields, decoderFields)
	}
	for field := range schemaSet {
		if _, ok := decoderSet[field]; !ok {
			t.Fatalf("%s field %q is required only by the schema", name, field)
		}
	}
}
