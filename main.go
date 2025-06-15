package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path" // For createNewFile
	"path/filepath"
	"strconv"
	"strings"
	"time"

	// For Ollama, we don't need the anthropic SDK directly for tool definitions
	// but we do need jsonschema for generating input schemas.
	"github.com/invopop/jsonschema"
)

// --- Tool Definition and Schema Generation (as per the article, adapted for local use) ---
type ToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"` // Using generic map for Ollama
	Function    func(input json.RawMessage) (string, error)
}

// GenerateSchema creates a JSON schema for a given Go type T.
// This schema is used to inform the LLM about the expected input structure for a tool.
func GenerateSchema[T any]() map[string]interface{} {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false,
		DoNotReference:           true,
	}
	var v T
	schema := reflector.Reflect(v)

	props := make(map[string]interface{})
	if schema.Properties != nil {
		// Corrected iteration for orderedmap
		for _, key := range schema.Properties.Keys() {
			val, ok := schema.Properties.Get(key)
			if !ok {
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

// --- Ollama specific types ---
// ...existing code...
type OllamaRequest struct {
	Model    string `json:"model"`
	Prompt   string `json:"prompt"`
	Stream   bool   `json:"stream"`
	System   string `json:"system"`
}

type OllamaResponse struct {
	Response string `json:"response"`
	Done     bool   `json:"done"`
}

type OllamaModelResponse struct {
	Models []struct {
		Name     string `json:"name"`
		Modified string `json:"modified_at"`
	} `json:"models"`
}


// --- Tool Implementations (ReadFile, ListFiles, EditFile, WriteFile) ---

// ReadFile tool
type ReadFileInput struct {
	Path string `json:"path" jsonschema_description:"The relative path of a file in the working directory."`
}

var ReadFileDefinition = ToolDefinition{
	Name:        "read_file",
	Description: "Read the contents of a given relative file path. Use this when you want to see what's inside a file. Do not use this with directory names.",
	InputSchema: GenerateSchema[ReadFileInput](),
	Function:    ReadFile,
}

func ReadFile(input json.RawMessage) (string, error) {
	var params ReadFileInput
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("failed to parse input for read_file: %v", err)
	}
	if params.Path == "" {
		return "", fmt.Errorf("path cannot be empty for read_file")
	}
	content, err := os.ReadFile(params.Path)
	if err != nil {
		return "", fmt.Errorf("failed to read file '%s': %v", params.Path, err)
	}
	return string(content), nil
}

// ListFiles tool
type ListFilesInput struct {
	Path string `json:"path,omitempty" jsonschema_description:"Optional relative path to list files from. Defaults to current directory if not provided."`
}

var ListFilesDefinition = ToolDefinition{
	Name:        "list_files",
	Description: "List files and directories at a given path. If no path is provided, lists files in the current directory.",
	InputSchema: GenerateSchema[ListFilesInput](),
	Function:    ListFiles,
}

func ListFiles(input json.RawMessage) (string, error) {
	var params ListFilesInput
	// Try to unmarshal, but it's okay if input is empty for list_files (defaults to current dir)
	_ = json.Unmarshal(input, &params) // Error can be ignored if params.Path remains default ""

	dir := "."
	if params.Path != "" {
		dir = params.Path
	}

	var files []string
	err := filepath.Walk(dir, func(currentPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(dir, currentPath)
		if err != nil {
			return err
		}
		if relPath == "." { // Skip the root directory itself
			return nil
		}
		if info.IsDir() {
			files = append(files, relPath+"/")
		} else {
			files = append(files, relPath)
		}
		return nil
	})

	if err != nil {
		return "", fmt.Errorf("failed to list files in '%s': %v", dir, err)
	}
	result, err := json.Marshal(files)
	if err != nil {
		return "", fmt.Errorf("failed to marshal file list: %v", err)
	}
	return string(result), nil
}

// EditFile tool (as per article)
type EditFileInput struct {
	Path   string `json:"path" jsonschema_description:"The path to the file"`
	OldStr string `json:"old_str" jsonschema_description:"Text to search for. If empty and file doesn't exist, creates new file with new_str as content."`
	NewStr string `json:"new_str" jsonschema_description:"Text to replace old_str with, or content for new file."`
}

var EditFileDefinition = ToolDefinition{
	Name: "edit_file",
	Description: `Make edits to a text file. Replaces 'old_str' with 'new_str' in the given file. 
If 'old_str' is empty and the file does not exist, it creates a new file with 'new_str' as content.
'old_str' and 'new_str' MUST be different if 'old_str' is not empty and the file exists.`,
	InputSchema: GenerateSchema[EditFileInput](),
	Function:    EditFile,
}

func EditFile(input json.RawMessage) (string, error) {
	var params EditFileInput
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("failed to parse input for edit_file: %v", err)
	}

	if params.Path == "" {
		return "", fmt.Errorf("path cannot be empty for edit_file")
	}

	content, err := os.ReadFile(params.Path)
	if err != nil {
		if os.IsNotExist(err) && params.OldStr == "" { // Create new file if old_str is empty and file doesn't exist
			// Use createNewFile which handles directory creation
			errWrite := os.WriteFile(params.Path, []byte(params.NewStr), 0644)
			if errWrite != nil {
				return "", fmt.Errorf("failed to create new file '%s': %v", params.Path, errWrite)
			}
			return fmt.Sprintf("Successfully created file %s", params.Path), nil
		}
		return "", fmt.Errorf("failed to read file '%s' for editing: %v", params.Path, err)
	}
	
	if params.OldStr == "" && len(content) > 0 { // File exists, old_str is empty - prevent accidental overwrite
		return "", fmt.Errorf("file '%s' exists but old_str is empty; specify old_str for replacement or use write_file to overwrite", params.Path)
	}
	
	if params.OldStr == params.NewStr && params.OldStr != "" { // old_str and new_str are same, and not for creation
		return "", fmt.Errorf("old_str and new_str must be different for edit_file when old_str is not empty")
	}

	oldContent := string(content)
	newContent := strings.Replace(oldContent, params.OldStr, params.NewStr, -1)

	if oldContent == newContent && params.OldStr != "" { // No change made, and we weren't trying to create a file
		return "", fmt.Errorf("old_str '%s' not found in file '%s'", params.OldStr, params.Path)
	}

	if err := os.WriteFile(params.Path, []byte(newContent), 0644); err != nil {
		return "", fmt.Errorf("failed to write changes to file '%s': %v", params.Path, err)
	}
	return fmt.Sprintf("Successfully edited file %s", params.Path), nil
}

// WriteFile tool (added previously)
type WriteFileInput struct {
	Path    string `json:"path" jsonschema_description:"The path to the file to write. If the file exists, it will be overwritten."`
	Content string `json:"content" jsonschema_description:"The content to write to the file."`
}

var WriteFileDefinition = ToolDefinition{
	Name:        "write_file",
	Description: "Write content to a file. If the file doesn't exist, it will be created. If the file exists, its contents will be overwritten.",
	InputSchema: GenerateSchema[WriteFileInput](),
	Function:    WriteFile,
}

func WriteFile(input json.RawMessage) (string, error) {
	var params WriteFileInput
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("failed to parse input for write_file: %v", err)
	}
	if params.Path == "" {
		return "", fmt.Errorf("path cannot be empty for write_file")
	}
	// Ensure directory exists
	dir := filepath.Dir(params.Path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", fmt.Errorf("failed to create directory '%s': %w", dir, err)
		}
	}
	if err := os.WriteFile(params.Path, []byte(params.Content), 0644); err != nil {
		return "", fmt.Errorf("failed to write file '%s': %v", params.Path, err)
	}
	return fmt.Sprintf("Successfully wrote %d bytes to %s", len(params.Content), params.Path), nil
}

// createNewFile helper function (from article, ensure it's present if EditFile uses it)
// Note: The WriteFile tool above already handles directory creation and writing.
// This function is specifically for the EditFile scenario where old_str is "" and file doesn't exist.
// However, the EditFile implementation was simplified to directly write.
// If createNewFile is still referenced by EditFile as per the article, it should be:
func createNewFile(filePath, content string) (string, error) {
	dir := path.Dir(filePath) // Use "path" for manipulating paths, "path/filepath" for OS-specific operations
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", fmt.Errorf("failed to create directory '%s': %w", dir, err)
		}
	}
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("failed to create file '%s': %w", filePath, err)
	}
	return fmt.Sprintf("Successfully created file %s", filePath), nil
}


// --- Agent Logic ---
type Agent struct {
	model          string
	getUserMessage func() (string, bool)
	tools         []ToolDefinition
	systemPrompt   string
}

func NewAgent(model string, getUserMessage func() (string, bool), agentTools []ToolDefinition, systemPrompt string) *Agent {
	return &Agent{
		model:          model,
		getUserMessage: getUserMessage,
		tools:         agentTools,
		systemPrompt:   systemPrompt,
	}
}

func (a *Agent) Run(ctx context.Context) error {
	conversation := []string{}
	fmt.Printf("Chat with %s (use 'ctrl-c' to quit)\n", a.model)

	readUserInput := true
	
	var sessionStartTime time.Time
	var sessionTotalTokens int
	var firstTokenReceivedInSession bool


	for {
		// ... existing Run loop logic ...
		var currentPromptText string // Store the text of the current prompt for the LLM

		if readUserInput {
			fmt.Print("\u001b[94mYou\u001b[0m: ")
			userInput, ok := a.getUserMessage()
			if !ok {
				break
			}
			currentPromptText = userInput
			conversation = append(conversation, fmt.Sprintf("User: %s", userInput))
		} else {
			// If not reading user input, it means we are following up after a tool execution.
			// The prompt to the LLM will be the accumulated conversation.
			// The last message in 'conversation' is the tool result.
			if len(conversation) > 0 {
				currentPromptText = conversation[len(conversation)-1] // Or construct a summary
			} else {
				// Should not happen if loop logic is correct
				fmt.Println("Warning: Attempting to run inference without prior input or tool result.")
				readUserInput = true
				continue
			}
		}


		toolsDesc := "You have the following tools available. Respond with 'tool: <tool_name>({<json_args>})' to use a tool.\n"
		for _, tool := range a.tools {
			toolSchemaBytes, _ := json.Marshal(tool.InputSchema) // Convert schema to string for prompt
			toolsDesc += fmt.Sprintf("- %s: %s. Input schema: %s\n", tool.Name, tool.Description, string(toolSchemaBytes))
		}
		
		// Construct the full prompt for Ollama
		// The actual prompt sent to Ollama's "prompt" field will be the latest user message or tool result.
		// The conversation history will be part of the system message or managed differently if Ollama supports chat history directly.
		// For now, let's keep it simple: the "prompt" is the latest turn.
		// The system prompt will contain the tool descriptions and overall instructions.

		// Reset stats for this inference call
		// inferenceStartTime := time.Now() // This was here, but sessionStartTime is better for overall TPS
		inferenceTokens := 0
		// firstTokenInInference := true // This was here, but firstTokenReceivedInSession is for the whole session

		if !firstTokenReceivedInSession {
			sessionStartTime = time.Now() // Start session timer on first actual inference attempt
		}

		fmt.Print("\u001b[93mAI\u001b[0m: ") // Yellow for AI
		// The 'promptPayload' to runInference should be the current turn's content.
		// The 'toolsDescription' is now part of the system prompt passed to runInference.
		llmResponseContent, err := a.runInference(ctx, currentPromptText, toolsDesc) // toolsDesc is now part of system prompt in runInference
		if err != nil {
			fmt.Printf("\nError running inference: %v\n", err)
			readUserInput = true 
			continue
		}

		toolCall := extractToolCall(llmResponseContent)
		if toolCall != "" {
			fmt.Printf("\n\u001b[92mtool\u001b[0m: %s\n", toolCall) 
			toolResult := a.executeTool(toolCall)
			fmt.Printf("\u001b[92mresult\u001b[0m: %s\n", toolResult) 
			
			conversation = append(conversation, fmt.Sprintf("Assistant: %s", llmResponseContent)) 
			conversation = append(conversation, fmt.Sprintf("System: Tool %s executed. Result: %s", toolCall, toolResult)) 
			readUserInput = false 
		} else {
			conversation = append(conversation, fmt.Sprintf("Assistant: %s", llmResponseContent))
			readUserInput = true 
		}
		fmt.Println() 

		inferenceTokens += len(strings.Fields(llmResponseContent)) 
		if inferenceTokens > 0 && !firstTokenReceivedInSession {
			firstTokenReceivedInSession = true
			// sessionStartTime is already set correctly at the start of the first inference
		}
		sessionTotalTokens += inferenceTokens
		
		if firstTokenReceivedInSession {
			durationSinceFirstToken := time.Since(sessionStartTime)
			if durationSinceFirstToken.Seconds() > 0 {
				tokensPerSecond := float64(sessionTotalTokens) / durationSinceFirstToken.Seconds()
				fmt.Printf("\u001b[90mStats: Total Tokens: %d, Time: %.2fs, TPS: %.2f\u001b[0m\n",
					sessionTotalTokens, durationSinceFirstToken.Seconds(), tokensPerSecond)
			}
		}
	}
	return nil
}

func (a *Agent) runInference(ctx context.Context, promptPayload string, toolsDescription string) (string, error) {
	// The system prompt now includes tool descriptions from the Agent struct
	// and specific instructions on how to call tools.
	effectiveSystemPrompt := a.systemPrompt + "\n" + toolsDescription

	reqBody := OllamaRequest{
		Model:  a.model,
		Prompt: promptPayload, // This is the user's message or latest part of conversation
		Stream: true,
		System: effectiveSystemPrompt, // System message for the LLM including tool info
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal Ollama request: %v", err)
	}

	resp, err := http.Post("http://localhost:11434/api/generate", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to make Ollama request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body) // Read body for more error info
		return "", fmt.Errorf("Ollama request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}
	
	var fullResponse strings.Builder
	reader := bufio.NewReader(resp.Body)

	for {
		line, err := reader.ReadString('\n')
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("error reading Ollama stream: %v", err)
		}

		var ollResp OllamaResponse
		if errUnmarshal := json.Unmarshal([]byte(line), &ollResp); errUnmarshal != nil {
			// Log problematic line and error, then continue if possible
			fmt.Printf("\nWarning: could not unmarshal Ollama response line: <%s>, error: %v\n", strings.TrimSpace(line), errUnmarshal)
			continue // Skip this line and try to process the next
		}

		fmt.Print(ollResp.Response) 
		fullResponse.WriteString(ollResp.Response)

		if ollResp.Done {
			break
		}
	}
	return fullResponse.String(), nil
}

func extractToolCall(response string) string {
    const toolPrefix = "tool: "
    // Normalize potential variations in LLM output for tool calls
    // For example, LLM might add newlines or extra spaces around the tool call.
    // We are looking for the prefix and then trying to parse the call.
    
    // Find the last occurrence of "tool:" in case of multiple attempts or noise
    lastIdx := strings.LastIndex(response, toolPrefix)
    if lastIdx == -1 {
        return "" // No "tool:" prefix found
    }

    // Extract the part after "tool: "
    potentialCall := response[lastIdx+len(toolPrefix):]
    
    // The LLM might put the tool call on a new line or add other text.
    // We need to find the actual call, which should look like `tool_name(JSON_ARGS)`.
    // Let's find the first occurrence of `tool_name(`
    // This is a heuristic. A more robust parser might be needed for complex LLM outputs.
    
    // A simple approach: assume the tool call is the first "word" followed by "(".
    // And that the arguments are enclosed in the matching ")".
    
    // Trim leading/trailing whitespace from the potential call
    trimmedCall := strings.TrimSpace(potentialCall)

    // Example: tool_name({"key": "value"})
    // We need to ensure we capture the full JSON argument structure.
    // The simplest way is to assume the LLM produces it correctly.
    // If the LLM output is `tool: read_file({"path": "file.txt"}) some other text`,
    // we want to extract `read_file({"path": "file.txt"})`.

    // Find the first opening parenthesis
    openParenIdx := strings.Index(trimmedCall, "(")
    if openParenIdx == -1 {
        return "" // No opening parenthesis found, not a valid call format
    }

    // Find the matching closing parenthesis. This is tricky if JSON args have nested parentheses.
    // For simplicity, we'll find the *last* closing parenthesis.
    // This assumes the JSON arguments themselves don't have unbalanced outer parentheses.
    closeParenIdx := strings.LastIndex(trimmedCall, ")")
    if closeParenIdx == -1 || closeParenIdx < openParenIdx {
        return "" // No closing parenthesis, or it's before the opening one
    }

    // The actual tool call string is from the start of the tool name to the closing parenthesis
    finalCall := trimmedCall[:closeParenIdx+1]
    
    // Basic validation: check if it looks like a function call
    if !strings.Contains(finalCall, "(") || !strings.HasSuffix(finalCall, ")") {
        return "" // Doesn't look like a tool call
    }
    
    return finalCall
}

func (a *Agent) executeTool(toolCallInstruction string) string {
    // toolCallInstruction is expected to be like: read_file({"path":"main.go"})
    // or list_files({}) or list_files()
    
    idxOpenParen := strings.Index(toolCallInstruction, "(")
    if idxOpenParen == -1 {
        return fmt.Sprintf("Error: Invalid tool call format. Missing '('. Got: %s", toolCallInstruction)
    }

    toolName := strings.TrimSpace(toolCallInstruction[:idxOpenParen])
    
    idxCloseParen := strings.LastIndex(toolCallInstruction, ")")
    if idxCloseParen == -1 || idxCloseParen < idxOpenParen {
        return fmt.Sprintf("Error: Invalid tool call format. Missing closing ')'. Got: %s", toolCallInstruction)
    }

    argsStr := strings.TrimSpace(toolCallInstruction[idxOpenParen+1 : idxCloseParen])
    
    var toolToExecute ToolDefinition // Changed from tools.ToolDefinition
    found := false
    for _, t := range a.tools {
        if t.Name == toolName {
            toolToExecute = t
            found = true
            break
        }
    }

    if !found {
        return fmt.Sprintf("Error: Tool '%s' not found.", toolName)
    }

    var rawInput json.RawMessage
    if argsStr == "" { // Handles tool_name()
        rawInput = json.RawMessage("{}") // Assume empty JSON object for no-arg calls
    } else {
        rawInput = json.RawMessage(argsStr)
    }
    
    // Validate if rawInput is valid JSON, especially if argsStr was not empty
    if argsStr != "" && !json.Valid(rawInput) {
        // Attempt to fix common LLM mistake: non-string values not quoted.
        // This is a very basic heuristic. For example, if it's just a path string.
        // If the schema expects a single string and we got `some/path.txt` instead of `{"path":"some/path.txt"}`.
        // This part is tricky and depends on how robust you want the parsing to be.
        // For now, we'll assume the LLM provides valid JSON or an empty string for args.
        // If it's not valid JSON, and not empty, it's an error.
        return fmt.Sprintf("Error: Tool arguments are not valid JSON: %s", argsStr)
    }

    result, err := toolToExecute.Function(rawInput)
    if err != nil {
        return fmt.Sprintf("Error executing tool '%s': %v", toolName, err)
    }
    return result
}


// --- Main Application Setup ---
func getSystemPrompt(agentType string) string {
	switch agentType {
	case "code":
		return "You are an expert programmer. You can use tools to interact with the file system. When you want to use a tool, respond *only* in the format 'tool: <tool_name>({<json_args>})'. For example: 'tool: read_file({\"path\":\"src/main.go\"})'. Do not add any other text before or after the tool call. If you are not using a tool, respond normally."
	case "explain":
		return "You are a technical expert. You can use tools. When you want to use a tool, respond *only* in the format 'tool: <tool_name>({<json_args>})'. If you are not using a tool, respond normally."
	default:
		return "You are a helpful AI assistant. You can use tools. When you want to use a tool, respond *only* in the format 'tool: <tool_name>({<json_args>})'. If you are not using a tool, respond normally."
	}
}

func getAvailableModels() ([]string, error) {
	resp, err := http.Get("http://localhost:11434/api/tags")
	if err != nil {
		return nil, fmt.Errorf("error getting models: %v", err)
	}
	defer resp.Body.Close()

	var modelResp OllamaModelResponse
	if err := json.NewDecoder(resp.Body).Decode(&modelResp); err != nil {
		return nil, fmt.Errorf("error decoding response: %v", err)
	}

	models := make([]string, 0, len(modelResp.Models))
	for _, model := range modelResp.Models {
		models = append(models, model.Name)
	}
	return models, nil
}

func selectModel() (string, error) {
	models, err := getAvailableModels()
	if err != nil {
		return "", err
	}

	if len(models) == 0 {
		return "", fmt.Errorf("no Ollama models available. Please ensure Ollama is running and models are pulled")
	}

	fmt.Println("\nAvailable models:")
	for i, model := range models {
		fmt.Printf("%d. %s\n", i+1, model)
	}

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("\nSelect a model (enter number): ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		var num int
		num, err = strconv.Atoi(input) // Ensure strconv is imported
		if err == nil && num > 0 && num <= len(models) {
			return models[num-1], nil
		}
		fmt.Println("Invalid selection. Please try again.")
	}
}

func main() {
	modelName := flag.String("model", "", "Name of the Ollama model to use (e.g., llama3:latest, codellama:latest)")
	agentType := flag.String("agent", "default", "Type of agent to use (default, code, explain)")
	// oneshot := flag.Bool("oneshot", false, "Run a single interaction without looping") // Can be added back
	// inputFile := flag.String("file", "", "Path to file containing the prompt") // Can be added back
	flag.Parse()

	selectedModel := *modelName
	if selectedModel == "" {
		var err error
		selectedModel, err = selectModel()
		if err != nil {
			fmt.Printf("Error selecting model: %v\n", err)
			return
		}
	}
	fmt.Printf("Using model: %s\n", selectedModel)

	scanner := bufio.NewScanner(os.Stdin)
	getUserMessage := func() (string, bool) {
		// if *inputFile != "" { ... } // Logic for inputFile can be re-added here
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				fmt.Printf("\nError reading input: %v\n", err)
			}
			return "", false 
		}
		return scanner.Text(), true
	}

	systemPrompt := getSystemPrompt(*agentType)
	availableTools := []ToolDefinition{ // Changed from tools.ToolDefinition
		ReadFileDefinition,
		ListFilesDefinition,
		EditFileDefinition,
		WriteFileDefinition,
	}

	agent := NewAgent(selectedModel, getUserMessage, availableTools, systemPrompt)
	if err := agent.Run(context.Background()); err != nil {
		fmt.Printf("\nAgent run failed: %v\n", err)
	}
}