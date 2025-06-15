package agent

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type OllamaError struct {
	Error string `json:"error"`
}

func makeOllamaRequest(reqBody map[string]interface{}) (*http.Response, error) {
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %v", err)
	}

	resp, err := http.Post("http://localhost:11434/api/generate", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		var ollError OllamaError
		if err := json.NewDecoder(resp.Body).Decode(&ollError); err != nil {
			return nil, fmt.Errorf("request failed with status %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("ollama error: %s", ollError.Error)
	}

	return resp, nil
}

type ollamaResponse struct {
	Response string `json:"response"`
	Done     bool   `json:"done"`
}

func processStream(resp *http.Response, stats *Stats) error {
	defer resp.Body.Close()
	reader := bufio.NewReader(resp.Body)
	firstToken := true

	for {
		line, err := reader.ReadString('\n')
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("error reading stream: %v", err)
		}

		var ollResp ollamaResponse
		if err := json.Unmarshal([]byte(line), &ollResp); err != nil {
			return fmt.Errorf("error unmarshaling response: %v", err)
		}

		if firstToken {
			stats.FirstTokenTime = time.Now()
			firstToken = false
		}

		fmt.Print(ollResp.Response)
		stats.TokenCount++

		if ollResp.Done {
			break
		}
	}
	return nil
}
