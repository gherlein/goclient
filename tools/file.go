package tools

import (
	"encoding/json"
	"fmt"
	"os"
)

type ReadFileInput struct {
	Path string `json:"path" jsonschema_description:"The relative path of a file in the working directory."`
}

var ReadFileTool = ToolDefinition{
	Name:        "read_file",
	Description: "Read the contents of a given relative file path. Use this when you want to see what's inside a file. Do not use this with directory names.",
	Function: func(input json.RawMessage) (string, error) {
		var params ReadFileInput
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("failed to parse input: %v", err)
		}

		content, err := os.ReadFile(params.Path)
		if err != nil {
			return "", fmt.Errorf("failed to read file: %v", err)
		}
		return string(content), nil
	},
}
