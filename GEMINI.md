# kfzf - Kubernetes Completion with FZF

## Project Overview

`kfzf` is a high-performance command-line tool that provides fast, fuzzy-searchable tab completion for `kubectl` using `fzf`. It solves the latency problem of standard kubectl completion by maintaining a background daemon that watches Kubernetes resources in real-time.

### Architecture

The system consists of two main components:

1.  **Server (Daemon)**:
    *   Runs in the background.
    *   Connects to the Kubernetes API using the user's kubeconfig.
    *   Maintains real-time `watch` streams for common resources (pods, services, etc.).
    *   Stores resources in a thread-safe in-memory cache for instant access.
    *   Serves completion requests via a Unix socket (`/tmp/kfzf.sock`).

2.  **Client (CLI)**:
    *   Invoked by the shell completion script (ZSH).
    *   Connects to the server via the Unix socket.
    *   Fetches cached resources and formats them for `fzf`.
    *   Can also be used directly to query the cache or manage the server.

## Building and Running

The project uses a `Makefile` for common tasks.

### Prerequisites
*   Go 1.21+
*   `fzf` installed
*   `kubectl` configured

### Key Commands

*   **Build**: `make build` (creates `kfzf` binary)
*   **Install**: `make install` (installs to `~/.local/bin`)
*   **Run Server (Debug)**: `make run-server` (runs in foreground with debug logging)
*   **Clean**: `make clean` (removes binary and socket file)

### Testing

*   **Unit Tests**: `make test` (runs standard Go tests)
*   **Integration Tests**: `make test-zsh` (runs ZSH completion tests, requires `zsh`)
*   **All Tests**: `make test-all`
*   **Linting**: `make lint` (runs `golangci-lint`)

## Codebase Structure

*   **`cmd/kfzf/`**: Entry point. Contains `main.go` and CLI command definitions using `cobra`.
*   **`internal/server/`**: Core daemon logic. Handles socket connections, protocol, and coordinates watchers.
*   **`internal/store/`**: In-memory cache implementation. Stores resources indexed by `context -> GVR -> namespace -> name`.
*   **`internal/k8s/`**: Kubernetes interaction.
    *   `watcher.go`: Manages API watch streams with backoff.
    *   `client.go`: Handles dynamic clients and kubeconfig loading.
    *   `discovery.go`: Resolves resource names/shortcuts to GroupVersionResources (GVR).
*   **`internal/fzf/`**: Formatter logic to generate the columnar output seen in `fzf`.
*   **`internal/config/`**: Configuration loading and saving (YAML).

## Development Conventions

*   **Language**: Go (1.25+ specified in `go.mod`).
*   **Style**: Standard Go idioms. Use `make lint` to ensure compliance.
*   **Configuration**: Uses YAML for user config (`~/.config/kfzf/config.yaml`). Supports custom column definitions for resources using JSONPath-like syntax.
*   **Error Handling**: The server is designed to be resilient. Watchers automatically retry with exponential backoff on failure.

## Usage (Quick Start)

1.  **Start Server**: `kfzf server &`
2.  **Get Completions**: `kfzf complete pods`
3.  **Check Status**: `kfzf status`
4.  **ZSH Integration**: Source the output of `kfzf zsh-completion` in your `.zshrc`.
