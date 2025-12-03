# kfzf - Kubernetes Completion with FZF

Fast kubectl completion using fzf with a background daemon that watches Kubernetes resources.

## Features

- **Real-time**: Uses Kubernetes watch API for instant updates
- **Fast**: In-memory cache means sub-5ms completion times
- **Multi-context**: Supports multiple kubeconfig contexts
- **Configurable**: YAML config for customizing displayed columns per resource type
- **Low overhead**: Single daemon serves all terminal sessions

## Installation

### Prerequisites

- Go 1.21+
- [fzf](https://github.com/junegunn/fzf)
- kubectl configured with valid kubeconfig

### Build from source

```bash
git clone https://github.com/pslijkhuis/kfzf.git
cd kfzf
make build
make install  # installs to ~/.local/bin
```

## Usage

### Start the server

The server runs in the background and watches Kubernetes resources:

```bash
# Start in background
kfzf server &

# Or run in foreground for debugging
kfzf server -f --log-level=debug
```

### Systemd user service (recommended)

For automatic startup, create a systemd user service:

```bash
mkdir -p ~/.config/systemd/user

cat > ~/.config/systemd/user/kfzf.service << 'EOF'
[Unit]
Description=kfzf - kubectl completion server
After=network.target

[Service]
Type=simple
ExecStart=%h/.local/bin/kfzf server -f
Restart=on-failure
RestartSec=5
Environment=KUBECONFIG=%h/.kube/config

[Install]
WantedBy=default.target
EOF

# Enable and start the service
systemctl --user daemon-reload
systemctl --user enable kfzf
systemctl --user start kfzf

# Check status
systemctl --user status kfzf

# View logs
journalctl --user -u kfzf -f
```

To use with merged kubeconfigs:

```bash
# If you merge multiple kubeconfigs, point to the merged file
Environment=KUBECONFIG=%h/.kube/flatten_config
```

### Get completions

```bash
# Get all pods
kfzf complete pods

# Get pods in specific namespace
kfzf complete pods -n kube-system

# Get pods with fzf selection
kfzf complete pods --fzf
```

### Check server status

```bash
kfzf status
kfzf status --json
```

### ZSH integration

Add to your `.zshrc`:

```bash
# Source the completion script
source <(kfzf zsh-completion)

# Start server if not running (optional)
if ! kfzf status &>/dev/null; then
  kfzf server &
fi
```

After sourcing, press `Ctrl+K` while typing a kubectl command to trigger fzf completion:

```bash
kubectl get pods <Ctrl+K>   # fzf opens with pod list
kubectl logs -n system <Ctrl+K>  # fzf opens with pods from 'system' namespace
```

## Configuration

Create a config file at `~/.config/kfzf/config.yaml`:

```bash
kfzf config init
```

### Example config

```yaml
server:
  socketPath: /tmp/kfzf.sock

resources:
  pods:
    columns:
      - name: NAME
        field: .metadata.name
        width: 50
      - name: NAMESPACE
        field: .metadata.namespace
        width: 20
      - name: STATUS
        field: .status.phase
        width: 12
      - name: AGE
        field: .metadata.creationTimestamp
        width: 10

  deployments:
    columns:
      - name: NAME
        field: .metadata.name
        width: 50
      - name: NAMESPACE
        field: .metadata.namespace
        width: 20
      - name: READY
        field: .status.readyReplicas/.spec.replicas
        width: 10
      - name: AGE
        field: .metadata.creationTimestamp
        width: 10
```

### Field syntax

- Simple path: `.metadata.name`
- Nested path: `.spec.containers[0].image`
- Ratio: `.status.readyReplicas/.spec.replicas`
- Array access: `.spec.rules[*].host`
- Filtered array: `.status.conditions[?(@.type=="Ready")].status`
- Age (special): `.metadata.creationTimestamp` (auto-formatted as age)

## Architecture

```
┌─────────────────┐     ┌──────────────────┐     ┌─────────────────┐
│  ZSH completion │────▶│  kfzf client     │────▶│  kfzf server    │
│  Ctrl+K widget  │     │  (unix socket)   │     │  (watches K8s)  │
└─────────────────┘     └──────────────────┘     └─────────────────┘
                                                         │
                                                         ▼
                                                 ┌─────────────────┐
                                                 │  K8s API        │
                                                 │  (watch streams)│
                                                 └─────────────────┘
```

The server:
1. Connects to the Kubernetes API using your kubeconfig
2. Establishes watch streams for commonly used resources
3. Maintains an in-memory index of all resources
4. Serves completion requests via unix socket

The client:
1. Connects to server via unix socket
2. Requests completions for a specific resource type/namespace
3. Receives formatted output instantly
4. Optionally pipes through fzf for selection

## Commands

```
kfzf server              # Start the daemon
kfzf complete <type>     # Get completions
kfzf status              # Show server status
kfzf refresh             # Reload kubeconfig
kfzf watch <types...>    # Start watching resources
kfzf watch --stop <types...>  # Stop watching
kfzf config init         # Create default config
kfzf config path         # Show config path
kfzf config show         # Show current config
kfzf zsh-completion      # Print ZSH integration script
```

## License

MIT
