# kfzf - Kubernetes Completion with FZF

Fast kubectl completion using fzf with a background daemon that watches Kubernetes resources.

## Features

- **Real-time**: Uses Kubernetes watch API for instant updates
- **Fast**: In-memory cache means sub-5ms completion times
- **Multi-context**: Supports multiple kubeconfig contexts
- **Configurable**: YAML config for customizing displayed columns per resource type
- **Low overhead**: Single daemon serves all terminal sessions
- **CRD support**: Built-in formatting for CloudNativePG, ArgoCD, Cluster API, cert-manager
- **Plugin support**: Completion for kubectl plugins like cnpg

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

## Quick Start

```bash
# Install systemd service (recommended)
kfzf systemd --install

# Add to ~/.zshrc
source <(kfzf zsh-completion)

# Use: type kubectl command and press Ctrl+K
kubectl get pods <Ctrl+K>
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

**Server flags:**
- `-f, --foreground`: Run in foreground (default: false)
- `--log-level`: Log level: debug, info, warn, error (default: info)

### Systemd user service (recommended)

For automatic startup, use the built-in systemd command:

```bash
# Preview the service file
kfzf systemd

# Install and enable the service
kfzf systemd --install

# Check status
systemctl --user status kfzf

# View logs
journalctl --user -u kfzf -f

# Uninstall the service
kfzf systemd --uninstall
```

### Get completions

```bash
# Get all pods
kfzf complete pods

# Get pods in specific namespace
kfzf complete pods -n kube-system

# Get pods from specific context
kfzf complete pods -c my-cluster

# Get pods with fzf selection
kfzf complete pods --fzf
```

**Complete flags:**
- `-n, --namespace`: Kubernetes namespace
- `-c, --context`: Kubernetes context (default: current)
- `--fzf`: Pipe output through fzf for interactive selection

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

# Start server if not running (optional, use systemd instead)
if ! kfzf status &>/dev/null; then
  kfzf server &
fi
```

After sourcing, press `Ctrl+K` while typing a kubectl command to trigger fzf completion:

```bash
kubectl get pods <Ctrl+K>        # fzf opens with pod list
kubectl logs -n system <Ctrl+K>  # fzf opens with pods from 'system' namespace
kubectl get pods -A <Ctrl+K>     # fzf opens with pods from all namespaces
```

### FZF Keybindings

While in the fzf selection window:

| Key | Action |
|-----|--------|
| `Tab` / `Shift+Tab` | Navigate up/down |
| `Ctrl+P` | Toggle preview window |
| `Ctrl+D` / `Ctrl+U` | Half-page scroll in main view |
| `Alt+D` / `Alt+U` | Half-page scroll in preview |
| `Alt+I` | Show resource description (kubectl describe) |
| `Ctrl+L` | Show logs (pods only) |
| `Ctrl+E` | Show events |
| `Ctrl+R` | Show rollout history (deployments/statefulsets/daemonsets) |
| `Ctrl+O` | Edit resource (kubectl edit) |
| `Ctrl+X` | Delete resource (kubectl delete) |
| `Ctrl+S` | Toggle selection (multi-select mode) |
| `Ctrl+A` | Toggle all (multi-select mode) |

### Context Isolation

For per-terminal context isolation (each shell gets its own kubeconfig copy):

```bash
# In your terminal
kfzf-isolate-context
```

This creates a copy of your kubeconfig that's isolated to the current shell session.

## Commands Reference

### Core Commands

```bash
kfzf server                    # Start the daemon
  -f, --foreground             # Run in foreground
  --log-level=<level>          # Log level (debug, info, warn, error)

kfzf complete <type>           # Get completions
  -n, --namespace=<ns>         # Kubernetes namespace
  -c, --context=<ctx>          # Kubernetes context
  --fzf                        # Pipe through fzf

kfzf status                    # Show server status
  --json                       # Output as JSON

kfzf refresh                   # Reload kubeconfig and clear caches

kfzf watch <types...>          # Start watching resource types
  -c, --context=<ctx>          # Kubernetes context
  --stop                       # Stop watching instead of starting

kfzf systemd                   # Show systemd service file
  --install                    # Install and enable service
  --uninstall                  # Stop and remove service
```

### Helper Commands

```bash
kfzf containers <pod-name>     # Get container names for a pod
  -n, --namespace=<ns>
  -c, --context=<ctx>

kfzf ports <resource-name>     # Get ports for a pod or service
  -n, --namespace=<ns>
  -c, --context=<ctx>
  -t, --type=<type>            # Resource type: pods or services (default: pods)

kfzf labels <resource-type>    # Get labels for a resource type
  -n, --namespace=<ns>
  -c, --context=<ctx>

kfzf field-values <type> <field>  # Get field values for field selector completion
  -n, --namespace=<ns>
  -c, --context=<ctx>
```

Supported fields for field-values: `metadata.name`, `metadata.namespace`, `spec.nodeName`, `spec.restartPolicy`, `spec.schedulerName`, `spec.serviceAccountName`, `status.phase`, `status.podIP`, `status.nominatedNodeName`

### Config Commands

```bash
kfzf config init               # Create default config file
kfzf config path               # Show config file path
kfzf config show               # Show current configuration
```

### Recent Resources

```bash
kfzf recent get <type>         # Get recently accessed resources
  -n, --namespace=<ns>
  -c, --context=<ctx>

kfzf recent record <type> <name>  # Record a resource as recently accessed
  -n, --namespace=<ns>
  -c, --context=<ctx>
```

### Other

```bash
kfzf zsh-completion            # Print ZSH integration script
kfzf --version                 # Show version
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

### Special field formatters

For certain CRDs, kfzf provides special formatters:

| Field | Description |
|-------|-------------|
| `_nodeStatus` | Node status with conditions |
| `_nodeTopology` | Node topology info |
| `_capiClusterStatus` | Cluster API cluster status |
| `_cnpgClusterReady` | CloudNativePG cluster ready indicator |
| `_cnpgClusterStatus` | CloudNativePG cluster status |
| `_certReady` | cert-manager certificate ready indicator |
| `_issuerReady` | cert-manager issuer ready indicator |

## Watched Resources

By default, kfzf watches these resource types:
- pods, services, configmaps, secrets
- namespaces, nodes
- deployments, statefulsets, daemonsets

### Auto-watch on first completion

When you try to complete a resource type that isn't being watched, kfzf automatically starts watching it. The first completion may be slow (fetching initial data), but subsequent completions are instant.

```bash
# First completion of replicasets - triggers auto-watch
kubectl get replicasets <Ctrl+K>  # starts watching, then completes

# Check what's being watched
kfzf status
```

### Manual watch management

You can also manage watches explicitly:

```bash
# Pre-watch resources for instant first completion
kfzf watch replicasets jobs cronjobs

# Stop watching resources to save memory
kfzf watch --stop replicasets

# Watch CRDs (use full resource.group format)
kfzf watch clusters.postgresql.cnpg.io applications.argoproj.io
```

## Built-in CRD Support

kfzf has pre-configured column layouts for these CRDs:

### CloudNativePG
- `clusters.postgresql.cnpg.io`: NAME, NAMESPACE, READY, STATUS, AGE

### ArgoCD
- `applications.argoproj.io`: NAME, NAMESPACE, SYNC, HEALTH, AGE
- `appprojects.argoproj.io`: NAME, NAMESPACE, AGE
- `applicationsets.argoproj.io`: NAME, NAMESPACE, AGE

### Cluster API
- `clusters.cluster.x-k8s.io`: NAME, NAMESPACE, STATUS, AGE

### cert-manager
- `certificates.cert-manager.io`: NAME, NAMESPACE, READY, SECRET, AGE
- `clusterissuers.cert-manager.io`: NAME, READY, AGE
- `issuers.cert-manager.io`: NAME, NAMESPACE, READY, AGE

## kubectl Plugin Support

kfzf supports completion for kubectl plugins:

### CloudNativePG (cnpg)

```bash
kubectl cnpg status -n postgres <Ctrl+K>  # completes CNPG clusters
kubectl cnpg promote -n postgres <Ctrl+K>
kubectl cnpg restart -n postgres <Ctrl+K>
```

Supported cnpg subcommands: status, promote, restart, reload, maintenance, fencing, hibernate, destroy, logs, pgbench, fio

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

**The server:**
1. Connects to the Kubernetes API using your kubeconfig
2. Establishes watch streams for commonly used resources
3. Maintains an in-memory index of all resources
4. Serves completion requests via unix socket
5. Automatically cleans up unused caches (30min idle)

**The client:**
1. Connects to server via unix socket
2. Requests completions for a specific resource type/namespace
3. Receives formatted output instantly
4. Optionally pipes through fzf for selection

## Troubleshooting

### Server not running

```bash
# Check if server is running
kfzf status

# Start server
kfzf server &

# Or check systemd status
systemctl --user status kfzf
```

### Stale cache after context switch

```bash
# Refresh kubeconfig and clear caches
kfzf refresh
```

### Debug mode

```bash
# Run server in foreground with debug logging
kfzf server -f --log-level=debug
```

### Socket issues

```bash
# Check socket
ls -la /tmp/kfzf.sock

# Remove stale socket (if server is not running)
rm /tmp/kfzf.sock
```

## License

MIT
