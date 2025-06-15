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
		httpClient:     &http.Client{Timeout: 60 * time.Second}, // Added a timeout
	}
}

func (a *Agent) Run(ctx context.Context) error {
	// For Ollama, conversation history is typically managed by resending messages.
	// Some models might support a 'context' field, but sending message history is more common.
	var conversationHistory []string // Stores "role: content" pairs or just content

	fmt.Printf("Chat with Ollama model %s (use 'ctrl-c' to quit)\n", a.modelName)

	for {
		fmt.Print("\u001b[94mYou\u001b[0m: ") // Blue for user
		userInput, ok := a.getUserMessage()
		if !ok {
			break // End of input or scanner error
		}

		// Add user input to history (simple append, Ollama doesn't have structured roles like Anthropic)
		conversationHistory = append(conversationHistory, userInput)

		// Construct the prompt for Ollama. Could be just the latest userInput,
		// or a concatenation of conversationHistory.
		// For simplicity, let's use the latest input as the main prompt,
		// and the system prompt for overall instructions.
		// More advanced: format conversationHistory into the prompt string.
		currentPrompt := userInput

		fmt.Print("\u001b[93mAI\u001b[0m: ") // Yellow for AI
		// Stats tracking for this inference
		inferenceStartTime := time.Now()
		var responseTokens int // Approximate based on words or implement proper tokenizer

		err := a.runInference(ctx, currentPrompt, conversationHistory, func(responsePart string) {
			fmt.Print(responsePart)
			responseTokens += len(strings.Fields(responsePart)) // Approximate token count
		})

		if err != nil {
			fmt.Printf("\nError during inference: %v\n", err)
			// Decide if to continue or break; for now, let's continue
			// Remove last user message from history if inference failed before AI response
			if len(conversationHistory) > 0 {
				// This is tricky, as AI might have started responding.
				// For now, we'll keep it to avoid losing user input.
			}
			continue
		}
		fmt.Println() // Newline after AI's full response

		// Add AI's (full) response to history - this part is tricky with streaming.
		// The runInference callback handles printing. We need the full response here.
		// For now, conversationHistory only stores user inputs.
		// To store AI responses, runInference would need to return the full response string.

		// Print stats
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

// runInference sends the prompt to Ollama and handles streaming response
func (a *Agent) runInference(ctx context.Context, prompt string, history []string, streamCallback func(responsePart string)) error {
	// Simple way to include history: prepend to the current prompt.
	// This might not be ideal for all models or long histories.
	// Some Ollama models might prefer a specific format or use a 'context' field.
	fullPrompt := strings.Join(history, "\n\n") // Join all history, then add current prompt
	if len(history) > 1 { // If there's actual history beyond the current prompt
		fullPrompt = strings.Join(history[:len(history)-1], "\n\n") + "\n\nUser: " + prompt
	} else {
		fullPrompt = "User: " + prompt
	}


	requestPayload := OllamaRequest{
		Model:  a.modelName,
		Prompt: fullPrompt, // Send the constructed prompt
		System: a.systemPrompt,
		Stream: true,
		// Messages: history, // Alternative way to send history if model supports it
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
		if err := json.Unmarshal(line, &ollamaResp); err != nil {
			fmt.Printf("\nWarning: could not unmarshal Ollama response line: %s, error: %v\n", string(line), err)
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
	flag.Parse()

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
	getUserMessage := func() (string, bool) {
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				fmt.Printf("\nError reading input: %v\n", err)
			}
			return "", false // End of input or error
		}
		return scanner.Text(), true
	}

	systemPrompt := getSystemPrompt(*agentTypeFlag)

	// Create and run the agent
	agent := NewAgent(selectedModelName, getUserMessage, systemPrompt)
	err := agent.Run(context.Background()) // Use context.Background() for simple cases
	if err != nil {
		fmt.Printf("Agent run failed: %s\n", err.Error())
	}
}