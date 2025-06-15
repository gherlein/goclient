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
	"strconv"
	"strings"
	"time"
)

// --- Ollama specific types ---
type OllamaRequest struct {
	Model    string   `json:"model"`
	Prompt   string   `json:"prompt"`
	System   string   `json:"system,omitempty"`
	Stream   bool     `json:"stream"`
	Messages []string `json:"messages,omitempty"` // For maintaining conversation history if model supports it
}

type OllamaResponse struct {
	Response string `json:"response"`
	Done     bool   `json:"done"`
	// Add other fields from Ollama's response as needed, e.g., context, eval_count, etc.
}

type OllamaModelInfo struct { // For listing models
	Name       string `json:"name"`
	ModifiedAt string `json:"modified_at"`
	Size       int64  `json:"size"`
}

type OllamaTagsResponse struct {
	Models []OllamaModelInfo `json:"models"`
}


// --- Agent Logic (Simplified for Ollama) ---
type Agent struct {
	modelName      string
	getUserMessage func() (string, bool)
	systemPrompt   string
	httpClient     *http.Client
}

func NewAgent(modelName string, getUserMessage func() (string, bool), systemPrompt string) *Agent {
	return &Agent{
		modelName:      modelName,
		getUserMessage: getUserMessage,
		systemPrompt:   systemPrompt,
		httpClient:     &http.Client{Timeout: 60 * time.Second},
	}
}

func (a *Agent) Run(ctx context.Context) error {
	var conversationHistory []string // Stores user inputs and AI responses for context

	fmt.Printf("Chat with Ollama model %s (type 'exit' to quit)\n", a.modelName)

	for {
		userInput, ok := a.getUserMessage() // This now handles its own prompting
		if !ok {
			break // End of input or scanner error
		}

		if strings.ToLower(strings.TrimSpace(userInput)) == "exit" {
			fmt.Println("Exiting chat.")
			break
		}

		// Add user input to history
		conversationHistory = append(conversationHistory, fmt.Sprintf("User: %s", userInput))

		// Construct the prompt for Ollama, including history
		// The runInference method will now receive the full history and format it.
		// The 'currentPrompt' is effectively the last user message.
		currentPrompt := userInput // For clarity, though runInference will use history

		fmt.Print("\u001b[93mAI\u001b[0m: ")
		inferenceStartTime := time.Now()
		var responseTokens int
		var fullAIReponse strings.Builder // To capture the full AI response for history

		err := a.runInference(ctx, currentPrompt, conversationHistory, func(responsePart string) {
			fmt.Print(responsePart)
			fullAIReponse.WriteString(responsePart) // Capture streamed parts
			responseTokens += len(strings.Fields(responsePart))
		})

		if err != nil {
			fmt.Printf("\nError during inference: %v\n", err)
			// Optionally remove the last user message from history if inference failed badly
			// conversationHistory = conversationHistory[:len(conversationHistory)-1]
			continue
		}
		fmt.Println() // Newline after AI's full response

		// Add AI's full response to history
		conversationHistory = append(conversationHistory, fmt.Sprintf("AI: %s", fullAIReponse.String()))


		duration := time.Since(inferenceStartTime)
		tps := 0.0
		if duration.Seconds() > 0 {
			tps = float64(responseTokens) / duration.Seconds()
		}
		fmt.Printf("\u001b[90mStats: Tokens: %d, Time: %.2fs, TPS: %.2f\u001b[0m\n",
			responseTokens, duration.Seconds(), tps)
	}
	return nil
}

func (a *Agent) runInference(ctx context.Context, currentPrompt string, history []string, streamCallback func(responsePart string)) error {
	// Construct the prompt for Ollama using the entire history.
	// The last element of history is the current user prompt.
	var promptForOllama strings.Builder
	for _, msg := range history {
		promptForOllama.WriteString(msg)
		promptForOllama.WriteString("\n\n") // Separate messages with double newlines
	}
	// Add a final "AI:" to signal the model to generate the AI's response.
	promptForOllama.WriteString("AI:")


	requestPayload := OllamaRequest{
		Model:  a.modelName,
		Prompt: promptForOllama.String(), // Send the full constructed prompt
		System: a.systemPrompt,
		Stream: true,
	}

	payloadBytes, err := json.Marshal(requestPayload)
	if err != nil {
		return fmt.Errorf("failed to marshal Ollama request: %v", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "http://localhost:11434/api/generate", bytes.NewBuffer(payloadBytes))
	if err != nil {
		return fmt.Errorf("failed to create Ollama request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request to Ollama: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Ollama request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadBytes('\n')
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("error reading stream from Ollama: %v", err)
		}

		var ollamaResp OllamaResponse
		if errUnmarshal := json.Unmarshal(line, &ollamaResp); errUnmarshal != nil {
			// Log problematic line and error, then continue if possible
			// This helps to see if Ollama is sending unexpected data.
			fmt.Printf("\nWarning: could not unmarshal Ollama response line: <%s>, error: %v\n", strings.TrimSpace(string(line)), errUnmarshal)
			continue 
		}

		streamCallback(ollamaResp.Response)

		if ollamaResp.Done {
			break
		}
	}
	return nil
}


// --- Main Application Setup ---

// getSystemPrompt can be used to set a default system message for Ollama
func getSystemPrompt(agentType string) string {
	switch agentType {
	case "code":
		return "You are an expert Go programmer. Provide clear and concise code examples."
	case "explain":
		return "You are a technical expert. Explain concepts clearly and thoroughly."
	default:
		return "You are a helpful AI assistant."
	}
}

// getAvailableOllamaModels fetches /api/tags from Ollama
func getAvailableOllamaModels(client *http.Client) ([]string, error) {
	req, err := http.NewRequest("GET", "http://localhost:11434/api/tags", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request for Ollama tags: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get Ollama tags: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Ollama /api/tags request failed with status %d", resp.StatusCode)
	}

	var tagsResp OllamaTagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tagsResp); err != nil {
		return nil, fmt.Errorf("failed to decode Ollama tags response: %v", err)
	}

	var modelNames []string
	for _, model := range tagsResp.Models {
		modelNames = append(modelNames, model.Name)
	}
	return modelNames, nil
}

// selectOllamaModel prompts user to select from available models
func selectOllamaModel(client *http.Client) (string, error) {
	models, err := getAvailableOllamaModels(client)
	if err != nil {
		return "", fmt.Errorf("could not fetch available Ollama models: %w", err)
	}
	if len(models) == 0 {
		return "", fmt.Errorf("no Ollama models found. Ensure Ollama is running and models are pulled (e.g., 'ollama pull llama3')")
	}

	fmt.Println("\nAvailable Ollama models:")
	for i, name := range models {
		fmt.Printf("%d. %s\n", i+1, name)
	}

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("Select a model by number: ")
		input, _ := reader.ReadString('\n')
		selection, err := strconv.Atoi(strings.TrimSpace(input))
		if err == nil && selection > 0 && selection <= len(models) {
			return models[selection-1], nil
		}
		fmt.Println("Invalid selection. Please try again.")
	}
}


func main() {
	// Command-line flags for Ollama model and agent type
	defaultModel := "llama3:latest" // A common default, user might need to change
	modelNameFlag := flag.String("model", "", fmt.Sprintf("Name of the Ollama model to use (e.g., llama3:latest, codellama:latest). If empty, you will be prompted to select."))
	agentTypeFlag := flag.String("agent", "code", "Type of agent behavior (default, code, explain)") // Changed default to "code"
	promptFileFlag := flag.String("promptfile", "", "Path to a file containing the initial prompt.") // New flag
	flag.Parse()

	var initialPromptFromFile string
	if *promptFileFlag != "" {
		content, err := os.ReadFile(*promptFileFlag)
		if err != nil {
			fmt.Printf("Warning: could not read prompt file '%s': %v. Proceeding with interactive input.\n", *promptFileFlag, err)
		} else {
			initialPromptFromFile = strings.TrimSpace(string(content))
		}
	}

	httpClient := &http.Client{Timeout: 30 * time.Second} // Client for model selection
	selectedModelName := *modelNameFlag

	if selectedModelName == "" {
		var err error
		selectedModelName, err = selectOllamaModel(httpClient)
		if err != nil {
			fmt.Printf("Error selecting Ollama model: %v\n", err)
			// Attempt to use a default if selection fails, or exit
			fmt.Printf("Attempting to use default model: %s\n", defaultModel)
			selectedModelName = defaultModel
			// Check if default model exists (optional, or let runInference fail)
		}
	}
	fmt.Printf("Using Ollama model: %s\n", selectedModelName)


	// Set up user input
	scanner := bufio.NewScanner(os.Stdin)
	isFilePromptUsed := false 

	getUserMessage := func() (string, bool) {
		var promptText string
		if initialPromptFromFile != "" && !isFilePromptUsed {
			fmt.Printf("\u001b[94mYou (from %s)\u001b[0m: %s\n", *promptFileFlag, initialPromptFromFile)
			isFilePromptUsed = true // Mark as used so it's not used again
			return initialPromptFromFile, true
		}

		// Standard prompt for stdin after initial file prompt (if any) or if no file prompt
		fmt.Print("\u001b[94mYou\u001b[0m: ")
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				fmt.Printf("\nError reading input: %v\n", err)
			}
			return "", false
		}
		promptText = scanner.Text()
		return promptText, true
	}

	systemPrompt := getSystemPrompt(*agentTypeFlag)

	// Create and run the agent
	agent := NewAgent(selectedModelName, getUserMessage, systemPrompt)
	err := agent.Run(context.Background()) // Use context.Background() for simple cases
	if err != nil {
		fmt.Printf("Agent run failed: %s\n", err.Error())
	}
}