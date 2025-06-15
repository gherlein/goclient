package tools

import (
	"encoding/json"

	"github.com/invopop/jsonschema"
)

// ToolDefinition defines the structure for a tool that the agent can use.
type ToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"` // Describes the expected JSON input for the tool
	Function    func(input json.RawMessage) (string, error) // The Go function that implements the tool
}

// GenerateSchema creates a JSON schema for a given Go type T.
// This schema is used to inform the LLM about the expected input structure for a tool.
func GenerateSchema[T any]() map[string]interface{} {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false, // Disallow unspecified fields in tool input
		DoNotReference:           true,  // Inline all schema definitions
	}
	var v T // Create an instance of T to reflect its structure
	schema := reflector.Reflect(v)

	props := make(map[string]interface{})
	if schema.Properties != nil {
		// Corrected: Use Keys() to get all keys, then Get(key) to retrieve each property schema
		for _, key := range schema.Properties.Keys() {
			val, ok := schema.Properties.Get(key)
			if !ok {
				// This case should ideally not happen if the key comes from Keys()
				continue
			}

			propSchema := make(map[string]interface{})
			propSchema["type"] = val.Type
			if val.Description != "" {
				propSchema["description"] = val.Description
			}
			props[key] = propSchema
		}
	}
	return props
}
	}
	return props
}
