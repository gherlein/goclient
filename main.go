package main

import (
	"bufio"
	"bytes"
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

type OllamaModelResponse struct {
	Models []struct {
		Name     string `json:"name"`
		Modified string `json:"modified_at"`
	} `json:"models"`
}

type OllamaRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
	System string `json:"system"`
}

type OllamaResponse struct {
	Response string `json:"response"`
	Done     bool   `json:"done"`
}

func getSystemPrompt(agentType string) string {
	switch agentType {
	case "code":
		return "You are an expert programmer. Provide clear, concise code solutions with explanations."
	case "explain":
		return "You are a technical expert. Explain concepts clearly and thoroughly."
	default:
		return "You are a helpful AI assistant. Be clear and concise in your responses."
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

	fmt.Println("\nAvailable models:")
	for i, model := range models {
		fmt.Printf("%d. %s\n", i+1, model)
	}

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("\nSelect a model (enter number): ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		if num, err := strconv.Atoi(input); err == nil && num > 0 && num <= len(models) {
			return models[num-1], nil
		}
		fmt.Println("Invalid selection. Please try again.")
	}
}

func main() {
	modelName := flag.String("model", "", "Name of the Ollama model to use")
	agentType := flag.String("agent", "default", "Type of agent to use (default, code, explain)")
	flag.Parse()

	selectedModel := *modelName
	if selectedModel == "" {
		var err error
		selectedModel, err = selectModel()
		if err != nil {
			fmt.Printf("Error selecting model: %v\n", err)
			return
		}
		fmt.Printf("Selected model: %s\n", selectedModel)
	}

	systemPrompt := getSystemPrompt(*agentType)

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Enter your prompt: ")
	prompt, _ := reader.ReadString('\n')
	prompt = strings.TrimSpace(prompt)

	reqBody := OllamaRequest{
		Model:  selectedModel,
		Prompt: prompt,
		Stream: true,
		System: systemPrompt,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		fmt.Printf("Error marshaling JSON: %v\n", err)
		return
	}

	resp, err := http.Post("http://localhost:11434/api/generate", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		fmt.Printf("Error making request: %v\n", err)
		return
	}
	defer resp.Body.Close()

	var startTime time.Time
	firstToken := true
	totalTokens := 0

	reader = bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadString('\n')
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Printf("Error reading response: %v\n", err)
			return
		}

		var ollResp OllamaResponse
		if err := json.Unmarshal([]byte(line), &ollResp); err != nil {
			fmt.Printf("Error unmarshaling JSON: %v\n", err)
			continue
		}

		if firstToken {
			startTime = time.Now()
			firstToken = false
		}

		fmt.Print(ollResp.Response)
		totalTokens++

		if ollResp.Done {
			break
		}
	}

	if !firstToken {
		duration := time.Since(startTime)
		tokensPerSecond := float64(totalTokens) / duration.Seconds()

		fmt.Printf("\n\nCompletion Stats:\n")
		fmt.Printf("Time: %.2f seconds\n", duration.Seconds())
		fmt.Printf("Tokens: %d\n", totalTokens)
		fmt.Printf("Tokens per second: %.2f\n", tokensPerSecond)
	}

}