package schema

import (
	"encoding/json"
	"testing"
)

// JSON-Schema documents commonly carry "additionalProperties": false. encoding/json matches
// field names case-insensitively, so without json:"-" that keyword bound to the Go field
// AdditionalProperties (as a bool) and the remaining-keys decode then failed with
// "expected type 'bool', got unconvertible type 'map[string]interface {}'".
func TestToolInputSchema_UnmarshalWithAdditionalPropertiesKeyword(t *testing.T) {
	doc := []byte(`{"type":"object","additionalProperties":false,"properties":{"projectId":{"type":"string"}},"required":["projectId"]}`)

	var in ToolInputSchema
	if err := json.Unmarshal(doc, &in); err != nil {
		t.Fatalf("unmarshal ToolInputSchema: %v", err)
	}
	if in.Type != "object" || len(in.Properties) != 1 || len(in.Required) != 1 {
		t.Fatalf("unexpected decode: %+v", in)
	}

	var out ToolOutputSchema
	if err := json.Unmarshal(doc, &out); err != nil {
		t.Fatalf("unmarshal ToolOutputSchema: %v", err)
	}

	encoded, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back map[string]interface{}
	_ = json.Unmarshal(encoded, &back)
	if _, bogus := back["AdditionalProperties"]; bogus {
		t.Fatalf("marshaled output contains the Go field name AdditionalProperties: %s", encoded)
	}
}
