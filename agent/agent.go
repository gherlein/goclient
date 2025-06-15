package agent

import (
	"fmt"
	"time"
)

type Stats struct {
	StartTime      time.Time
	TokenCount     int
	FirstTokenTime time.Time
}

type Agent struct {
	Model     string
	SystemMsg string
}

func NewAgent(model, systemMsg string) *Agent {
	return &Agent{
		Model:     model,
		SystemMsg: systemMsg,
	}
}

func (a *Agent) ProcessInference(prompt string, stats *Stats) error {
	// Create Ollama request
	reqBody := map[string]interface{}{
		"model":  a.Model,
		"prompt": prompt,
		"stream": true,
		"system": a.SystemMsg,
	}

	response, err := makeOllamaRequest(reqBody)
	if err != nil {
		return fmt.Errorf("inference request failed: %v", err)
	}

	return processStream(response, stats)
}

func (a *Agent) CallTool(name string, args map[string]interface{}) (interface{}, error) {
	switch name {
	case "search_docs":
		return searchDocs(args)
	case "get_file_content":
		return getFileContent(args)
	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}
	case "get_file_content":
		return getFileContent(args)
	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}
