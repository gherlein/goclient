# Go Ollama CLI Chat Agent

This project is a command-line interface (CLI) application written in Go that allows users to interact with local language models hosted by Ollama. It provides a simple chat interface in the terminal, maintaining conversation context and displaying basic performance statistics.

## Some Results

All the results below were taken using the same prompt (./prompt.txt) using the qwen2.5-coder:7b model in Ollama.

### MacBook Pro, M4 Pro, 48GB Unified Ram

```bash
Stats: Tokens: 864, Time: 23.19s, TPS: 37.26
```

### Beelink SER9 Pro AI Mini PC, AMD Ryzen AI 9 HX 370(80TOPS,12C/24T,5.1GHz), 64G LPDDR5X 8000MHz 2TB PCIe4.0 x4 SSD, AI PC AMD Radeon 890M NPU 50 AI Tops

```bash
Stats: Tokens: 612, Time: 39.57s, TPS: 15.47
```

(NOTE:  turns out that AMD ROCm does not support the GPU on that host, so this is all CPU)


Lenovo ThinkStation P620 Workstation, AMD Ryzen Threadripper PRO 3945WX 4.0GHz (12 Core), 32 GB DDR4 RAM, 1 TB SSD, Quadro P2000 5GB Graphics Card

```bash
Stats: Tokens: 824, Time: 39.83s, TPS: 20.69
```

## Features

*   **Ollama Integration**: Connects to a local Ollama instance to run inference with various language models.
*   **Model Selection**:
    *   If no model is specified via command-line, the application queries Ollama for available models and prompts the user to select one.
    *   Users can specify a model directly using the `-model` flag.
*   **Agent Behavior**: Supports different "agent types" (e.g., `code`, `explain`, `default`) via the `-agent` flag, which sets a system prompt to guide the LLM's behavior. The default agent behavior is `code`.
*   **Interactive Chat**:
    *   Users can type messages in the terminal to interact with the selected Ollama model.
    *   The conversation context is maintained across multiple turns.
    *   Users can type "exit" to end the chat session.
*   **Initial Prompt from File**: Supports an optional `-promptfile` command-line argument. If provided, the content of this file is used as the initial prompt to the LLM.
*   **Streaming Responses**: Displays the LLM's response as it's being generated (streamed).
*   **Performance Statistics**: After each AI response, it shows:
    *   Approximate number of tokens in the response.
    *   Time taken for the inference.
    *   Tokens per second (TPS).

## Prerequisites

*   [Go](https://go.dev/) (version 1.21 or later recommended)
*   [Ollama](https://ollama.com/) installed and running locally.
*   At least one model pulled via Ollama (e.g., `ollama pull llama3`).

## Setup

1.  **Clone the repository (if applicable) or ensure you have the project files.**
2.  **Navigate to the project directory:**
    ```bash
    cd /path/to/goclient 
    ```
3.  **Tidy dependencies:**
    ```bash
    go mod tidy
    ```
4.  **Build the application:**
    A `Makefile` is provided for convenience.
    ```bash
    make build
    ```
    Alternatively, build directly:
    ```bash
    go build -o goclient .
    ```

## Usage

Run the compiled binary from your terminal.

**Basic interactive chat (will prompt for model selection if none is running/default):**
```bash
./goclient
```

**Specify a model:**
```bash
./goclient -model llama3:latest
```

**Specify agent behavior (default is "code"):**
```bash
./goclient -agent explain -model mistral:latest
```

**Use an initial prompt from a file:**
Create a file, e.g., `my_prompt.txt`, with your desired initial prompt.
```bash
./goclient -promptfile my_prompt.txt -model codellama:latest
```

**Example Interaction:**
```
$ ./goclient -model llama3:latest -agent code
Using Ollama model: llama3:latest
Chat with Ollama model llama3:latest (type 'exit' to quit)
You: Write a simple Go function to add two numbers.
AI: Sure, here's a simple Go function to add two numbers:

```go
package main

import "fmt"

// add takes two integers and returns their sum.
func add(a int, b int) int {
	return a + b
}

func main() {
	sum := add(5, 3)
	fmt.Printf("The sum of 5 and 3 is: %d\n", sum) // Output: The sum of 5 and 3 is: 8
}
```
This function `add` takes two integer parameters, `a` and `b`, and returns their sum as an integer. The `main` function demonstrates how to call `add` and print the result.

Stats: Tokens: 90, Time: 1.50s, TPS: 60.00
You: exit
Exiting chat.
```

## Makefile Targets

*   `make build`: Builds the `goclient` binary.
*   `make run`: Builds and runs the application with default settings (prompts for model, agent is "code").
*   `make clean`: Removes the built binary.
*   `make fmt`: Formats the Go source code.
*   `make deps`: Runs `go mod tidy`.

## Code Overview

The application is contained within `main.go`. Key components include:

*   **`Agent` struct**: Manages the chat session, including the selected model, system prompt, and user input handling.
*   **`Agent.Run()`**: The main loop for the chat interaction. It gets user input, sends it to Ollama, and processes the response.
*   **`Agent.runInference()`**: Handles the HTTP communication with the Ollama `/api/generate` endpoint, including streaming.
*   **Model Selection**: Functions `getAvailableOllamaModels` and `selectOllamaModel` interact with Ollama's `/api/tags` endpoint.
*   **Command-line Flags**: Uses the `flag` package to parse arguments for model name, agent type, and initial prompt file.

This project serves as a foundational example of how to build a CLI chat application that interfaces with local LLMs via Ollama.


