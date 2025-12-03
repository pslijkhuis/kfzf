# kfzf kubectl completion for ZSH
# Add to your .zshrc: source <(kfzf completion zsh)

# Helper to get current context without ANSI color codes (kubectx adds colors)
_kfzf_current_context() {
  kubectl config current-context 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g'
}

# Per-shell kubeconfig isolation
# Call kfzf-isolate-context to enable per-shell context switching
# Each shell gets its own kubeconfig copy, so kubectx/kubectl config use-context
# only affects that shell
kfzf-isolate-context() {
  local shell_config="/tmp/kubeconfig-shell-$$"

  # If already isolated and file exists, skip
  if [[ "$KUBECONFIG" == "$shell_config" && -f "$shell_config" ]]; then
    echo "Already using per-shell kubeconfig: $shell_config"
    return 0
  fi

  # Find source kubeconfig - use saved original or current KUBECONFIG
  local source_config="${_KFZF_ORIGINAL_KUBECONFIG:-${KUBECONFIG:-$HOME/.kube/config}}"

  # If pointing to a stale/missing temp file, fall back to default
  if [[ "$source_config" == /tmp/kubeconfig-shell-* && ! -f "$source_config" ]]; then
    source_config="$HOME/.kube/config"
  fi

  # Save the original source for future re-isolation
  export _KFZF_ORIGINAL_KUBECONFIG="$source_config"

  # Handle multiple kubeconfig files (colon-separated) - use first one
  if [[ "$source_config" == *:* ]]; then
    source_config="${source_config%%:*}"
  fi

  if [[ ! -f "$source_config" ]]; then
    echo "Error: Source kubeconfig not found: $source_config" >&2
    return 1
  fi

  # Copy to per-shell file
  if ! cp "$source_config" "$shell_config" 2>/dev/null; then
    echo "Error: Failed to copy kubeconfig" >&2
    return 1
  fi
  chmod 600 "$shell_config"
  export KUBECONFIG="$shell_config"

  # Store path globally for cleanup
  export _KFZF_SHELL_KUBECONFIG="$shell_config"

  # Register cleanup on shell exit
  _kfzf_cleanup_kubeconfig() {
    [[ -n "$_KFZF_SHELL_KUBECONFIG" && -f "$_KFZF_SHELL_KUBECONFIG" ]] && \
      rm -f "$_KFZF_SHELL_KUBECONFIG" 2>/dev/null
  }
  add-zsh-hook zshexit _kfzf_cleanup_kubeconfig 2>/dev/null || \
    trap "_kfzf_cleanup_kubeconfig" EXIT

  echo "Per-shell kubeconfig enabled: $shell_config"
  echo "Current context: $(_kfzf_current_context)"
}

# Auto-isolate if KFZF_AUTO_ISOLATE is set
if [[ -n "$KFZF_AUTO_ISOLATE" ]]; then
  kfzf-isolate-context
fi

# Helper: run fzf with standard options
_kfzf_fzf() {
  local header=$1
  local prompt=$2
  local query=${3:-}
  local multi=${4:-}
  local preview_cmd=${5:-}

  local fzf_args=(
    --ansi
    --header="$header"
    --header-first
    --no-hscroll
    --tabstop=4
    --exit-0
    --border=rounded
    --prompt="$prompt"
    --pointer="▶ "
    --marker="★ "
    --color="header:italic:cyan,prompt:bold:blue,pointer:bold:magenta,marker:green"
    --layout=reverse
  )
  if [[ -n "$query" ]]; then
    # Use exact prefix matching when we have a query (user typed partial text)
    fzf_args+=(--query="^$query")
  fi
  # Always add tab navigation bindings
  fzf_args+=(--bind "tab:down,shift-tab:up")
  if [[ -n "$multi" ]]; then
    fzf_args+=(--multi)
    fzf_args+=(--bind "ctrl-s:toggle+down,ctrl-a:toggle-all")
  else
    fzf_args+=(--select-1)
  fi
  # Add preview if provided
  if [[ -n "$preview_cmd" ]]; then
    fzf_args+=(--preview="$preview_cmd")
    fzf_args+=(--preview-window=bottom:50%:wrap:follow)
    fzf_args+=(--bind "ctrl-p:toggle-preview")
  fi

  fzf "${fzf_args[@]}" 2>/dev/tty
}

# Helper: run fzf with switchable preview modes for resources
_kfzf_fzf_resource() {
  local header=$1
  local prompt=$2
  local query=${3:-}
  local multi=${4:-}
  local resource_type=$5
  local namespace=$6
  local context=$7

  # Capture stdin (piped data) for use with colorize
  local input
  input=$(cat)

  local ctx_arg=""
  [[ -n "$context" ]] && ctx_arg="--context $context"

  # Create temp script for preview that supports mode switching
  local preview_script=$(mktemp)
  cat > "$preview_script" << 'PREVIEW_EOF'
#!/bin/bash
name=$(echo "$1" | awk '{print $1}')
ns_col=$(echo "$1" | awk '{print $2}')
resource_type="$2"
namespace="$3"
context="$4"
mode="$5"

ctx_arg=""
[[ -n "$context" ]] && ctx_arg="--context $context"

# Use provided namespace or extract from column
if [[ -n "$namespace" ]]; then
  ns="$namespace"
else
  ns="$ns_col"
fi

ns_arg=""
[[ -n "$ns" ]] && ns_arg="-n $ns"

case "$mode" in
  logs)
    case "$resource_type" in
      pods|pod|po|pods.v1|pods.*)
        kubectl $ctx_arg logs -f $ns_arg "$name" --tail=50 2>/dev/null | bat --style=plain --color=always --language=log --paging=never
        ;;
      *)
        echo "Logs only available for pods (got: $resource_type)"
        ;;
    esac
    ;;
  events)
    case "$resource_type" in
      namespaces|namespace|ns)
        kubectl $ctx_arg get events -n "$name" --sort-by='.lastTimestamp' 2>&1
        ;;
      *)
        kubectl $ctx_arg get events $ns_arg --field-selector "involvedObject.name=$name" --sort-by='.lastTimestamp' 2>&1
        ;;
    esac
    ;;
  rollout)
    case "$resource_type" in
      deployments|deployment|deploy|deployments.apps)
        kubectl $ctx_arg rollout history deployment $ns_arg "$name" 2>/dev/null | bat --style=plain --color=always --language=yaml
        ;;
      statefulsets|statefulset|sts|statefulsets.apps)
        kubectl $ctx_arg rollout history statefulset $ns_arg "$name" 2>/dev/null | bat --style=plain --color=always --language=yaml
        ;;
      daemonsets|daemonset|ds|daemonsets.apps)
        kubectl $ctx_arg rollout history daemonset $ns_arg "$name" 2>/dev/null | bat --style=plain --color=always --language=yaml
        ;;
      *)
        echo "Rollout history only available for deployments/statefulsets/daemonsets"
        ;;
    esac
    ;;
  edit)
    kubectl $ctx_arg edit "$resource_type" $ns_arg "$name"
    ;;
  delete)
    echo "Delete $resource_type/$name in namespace $ns? (y/N)"
    read -r confirm < /dev/tty
    if [[ "$confirm" == "y" || "$confirm" == "Y" ]]; then
      kubectl $ctx_arg delete "$resource_type" $ns_arg "$name"
      echo "Deleted $resource_type/$name"
    else
      echo "Cancelled"
    fi
    ;;
  *)
    kubectl $ctx_arg describe "$resource_type" $ns_arg "$name" 2>/dev/null | bat --style=plain --color=always --language=yaml
    ;;
esac
PREVIEW_EOF
  chmod +x "$preview_script"

  local preview_cmd="$preview_script {} '$resource_type' '$namespace' '$context' logs"

  local fzf_args=(
    --ansi
    --header="$header"
    --header-first
    --no-hscroll
    --tabstop=4
    --exit-0
    --border=rounded
    --prompt="$prompt"
    --pointer="▶ "
    --marker="★ "
    --color="header:italic:cyan,prompt:bold:blue,pointer:bold:magenta,marker:green"
    --layout=reverse
    --preview="$preview_cmd"
    --preview-window=bottom:50%:wrap:follow
    --bind "ctrl-p:toggle-preview"
    --bind "ctrl-d:half-page-down"
    --bind "ctrl-u:half-page-up"
    --bind "alt-d:preview-half-page-down"
    --bind "alt-u:preview-half-page-up"
    --bind "alt-i:change-preview($preview_script {} '$resource_type' '$namespace' '$context' describe)+change-preview-window(bottom:50%:wrap)"
    --bind "ctrl-l:change-preview($preview_script {} '$resource_type' '$namespace' '$context' logs)+change-preview-window(bottom:50%:wrap:follow)"
    --bind "ctrl-e:change-preview($preview_script {} '$resource_type' '$namespace' '$context' events)+change-preview-window(bottom:50%:wrap)"
    --bind "ctrl-r:change-preview($preview_script {} '$resource_type' '$namespace' '$context' rollout)+change-preview-window(bottom:50%:wrap)"
    --bind "ctrl-o:execute($preview_script {} '$resource_type' '$namespace' '$context' edit)"
    --bind "ctrl-x:execute($preview_script {} '$resource_type' '$namespace' '$context' delete)"
  )

  if [[ -n "$query" ]]; then
    fzf_args+=(--query="^$query")
  fi
  fzf_args+=(--bind "tab:down,shift-tab:up")
  if [[ -n "$multi" ]]; then
    fzf_args+=(--multi)
    fzf_args+=(--bind "ctrl-s:toggle+down,ctrl-a:toggle-all")
  else
    fzf_args+=(--select-1)
  fi

  local result
  result=$(echo "$input" | fzf "${fzf_args[@]}" 2>/dev/tty)
  rm -f "$preview_script"
  echo "$result"
}

# Helper: extract first column from fzf result (handles multiple lines)
_kfzf_extract_name() {
  local result=$1
  if [[ -n "$result" ]]; then
    local names=()
    while IFS= read -r line; do
      [[ -z "$line" ]] && continue
      # Extract first column (before tab), trim whitespace
      local name=$(echo "${line%%$'	'*}" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
      [[ -n "$name" ]] && names+=("$name")
    done <<< "$result"
    echo "${names[*]}"
  fi
}

# Complete namespaces
_kfzf_complete_namespace() {
  local query=${1:-}
  local context=${2:-}

  # Use provided context or get from shell's kubectl (strip ANSI codes)
  local ctx="${context:-$(_kfzf_current_context)}"

  # Get recent namespaces to show first
  local recent_names
  if [[ -n "$ctx" ]]; then
    recent_names=$(kfzf recent get namespaces -c "$ctx" 2>/dev/null)
  else
    recent_names=$(kfzf recent get namespaces 2>/dev/null)
  fi

  local result
  if [[ -n "$recent_names" ]]; then
    # Get all completions
    local all_completions
    if [[ -n "$ctx" ]]; then
      all_completions=$(kfzf complete namespaces -c "$ctx" 2>/dev/null)
    else
      all_completions=$(kfzf complete namespaces 2>/dev/null)
    fi

    # Extract recent items and the rest
    local recent_lines=""
    local rest_lines="$all_completions"
    local match=""
    while IFS= read -r name; do
      [[ -z "$name" ]] && continue
      # Match name at start of line, followed by spaces/tab
      match=$(echo "$all_completions" | grep "^${name}[[:space:]]" | head -1)
            if [[ -n "$match" ]]; then
              recent_lines="${recent_lines}${match}"$'\n'
        rest_lines=$(echo "$rest_lines" | grep -v "^${name}[[:space:]]")
      fi
    done <<< "$recent_names"

    # Combine: recent first, then rest
    result=$(printf "%s%s" "$recent_lines" "$rest_lines" | _kfzf_fzf "ctx: $ctx | namespaces" "ns > " "$query")
  else
    if [[ -n "$ctx" ]]; then
      result=$(kfzf complete namespaces -c "$ctx" 2>/dev/null | _kfzf_fzf "ctx: $ctx | namespaces" "ns > " "$query")
    else
      result=$(kfzf complete namespaces 2>/dev/null | _kfzf_fzf "ctx: $ctx | namespaces" "ns > " "$query")
    fi
  fi

  if [[ -z "$result" ]]; then
    return
  fi

  # Record selected namespace
  local selected_name
  selected_name=$(_kfzf_extract_name "$result")
  if [[ -n "$selected_name" ]]; then
    if [[ -n "$ctx" ]]; then
      kfzf recent record namespaces "$selected_name" -c "$ctx" 2>/dev/null
    else
      kfzf recent record namespaces "$selected_name" 2>/dev/null
    fi
  fi

  echo "$selected_name"
}

# Complete contexts with cluster info preview
_kfzf_complete_context() {
  local query=${1:-}

  # Preview shows context details and cluster info
  local preview_cmd='ctx={}; echo "=== Context: $ctx ==="; kubectl config get-contexts "$ctx" 2>/dev/null; echo ""; echo "=== Cluster Info ==="; kubectl --context "$ctx" cluster-info 2>/dev/null | head -5; echo ""; echo "=== Nodes ==="; kubectl --context "$ctx" get nodes -o wide 2>/dev/null | head -10'

  local current=$(_kfzf_current_context)
  local header="Current: $current | Select context to switch"

  local fzf_args=(
    --ansi
    --header="$header"
    --header-first
    --no-hscroll
    --tabstop=4
    --exit-0
    --border=rounded
    --prompt="ctx > "
    --pointer="▶ "
    --marker="★ "
    --color="header:italic:cyan,prompt:bold:blue,pointer:bold:magenta,marker:green"
    --layout=reverse
    --preview="$preview_cmd"
    --preview-window=bottom:50%:wrap
    --bind "ctrl-p:toggle-preview"
    --bind "tab:down,shift-tab:up"
    --select-1
  )
  [[ -n "$query" ]] && fzf_args+=(--query="^$query")

  local result
  result=$(kubectl config get-contexts -o name 2>/dev/null | fzf "${fzf_args[@]}" 2>/dev/tty)
  echo "$result"
}

# Complete resources
# Args: resource_type namespace context query all_namespaces_mode
# When all_namespaces_mode=1, returns "NS:name" format for each selected resource
_kfzf_complete_resource() {
  local resource_type=$1
  local namespace=$2
  local context=$3
  local query=${4:-}
  local all_ns_mode=${5:-0}

  local cmd="kfzf complete $resource_type"
  [[ -n "$namespace" ]] && cmd="$cmd -n $namespace"
  [[ -n "$context" ]] && cmd="$cmd -c $context"

  local current_ctx=$(_kfzf_current_context)
  local header="ctx: ${context:-$current_ctx}"
  [[ -n "$namespace" ]] && header="$header | ns: $namespace"
  header="$header | $resource_type"
  header="$header | alt-i:info ctrl-l:logs ctrl-e:events ctrl-r:rollout ctrl-o:edit ctrl-x:delete"

  # Get recent resources to show first
  local recent_cmd="kfzf recent get $resource_type"
  [[ -n "$namespace" ]] && recent_cmd="$recent_cmd -n $namespace"
  [[ -n "$context" ]] && recent_cmd="$recent_cmd -c $context"
  local recent_names
  recent_names=$(eval "$recent_cmd" 2>/dev/null)

  local result
  if [[ -n "$recent_names" ]]; then
    # Get all completions
    local all_completions
    all_completions=$(eval "$cmd" 2>/dev/null)

    # Extract recent items and the rest
    local recent_lines=""
    local rest_lines="$all_completions"
    local match=""
    while IFS= read -r name; do
      [[ -z "$name" ]] && continue
      # Match name at start of line, followed by spaces/tab
      match=$(echo "$all_completions" | grep "^${name}[[:space:]]" | head -1)
      if [[ -n "$match" ]]; then
        recent_lines="${recent_lines}${match}"$'\n'
        rest_lines=$(echo "$rest_lines" | grep -v "^${name}[[:space:]]")
      fi
    done <<< "$recent_names"

    # Combine: recent first, then rest
    result=$(printf "%s%s" "$recent_lines" "$rest_lines" | _kfzf_fzf_resource "$header" "Select $resource_type > " "$query" "multi" "$resource_type" "$namespace" "$context")
  else
    result=$(eval "$cmd" 2>/dev/null | _kfzf_fzf_resource "$header" "Select $resource_type > " "$query" "multi" "$resource_type" "$namespace" "$context")
  fi

  if [[ -z "$result" ]]; then
    return
  fi

  # Record selected resources as recent
  local selected_names
  if [[ "$all_ns_mode" == "1" && -z "$namespace" ]]; then
    local output=()
    while IFS= read -r line; do
      [[ -z "$line" ]] && continue
      # Use tab as field separator and trim whitespace
      local name=$(echo "$line" | awk -F'\t' '{gsub(/^[[:space:]]+|[[:space:]]+$/, "", $1); print $1}')
      local ns=$(echo "$line" | awk -F'\t' '{gsub(/^[[:space:]]+|[[:space:]]+$/, "", $2); print $2}')
      [[ -n "$name" && -n "$ns" ]] && output+=("${ns}:${name}")
      # Record each selection
      kfzf recent record "$resource_type" "$name" -n "$ns" ${context:+-c "$context"} 2>/dev/null &
    done <<< "$result"
    echo "${output[*]}"
  else
    selected_names=$(_kfzf_extract_name "$result")
    # Record each selection (names are space-separated)
    for name in ${(z)selected_names}; do
      [[ -z "$name" ]] && continue
      eval "kfzf recent record $resource_type $name ${namespace:+-n $namespace} ${context:+-c $context}" 2>/dev/null
    done
    echo "$selected_names"
  fi
}

# Complete containers for a pod (uses cached data from server)
_kfzf_complete_container() {
  local pod=$1
  local namespace=$2
  local context=$3
  local query=${4:-}

  local -a kfzf_args=(containers "$pod")
  [[ -n "$namespace" ]] && kfzf_args+=(-n "$namespace")
  [[ -n "$context" ]] && kfzf_args+=(-c "$context")

  local containers
  containers=$(kfzf "${kfzf_args[@]}" 2>/dev/null)

  if [[ -z "$containers" ]]; then
    return
  fi

  local current_ctx=$(_kfzf_current_context)
  local header="ctx: ${context:-$current_ctx}"
  [[ -n "$namespace" ]] && header="$header | ns: $namespace"
  header="$header | pod: $pod | containers"

  local result
  result=$(echo "$containers" | _kfzf_fzf "$header" "container > " "$query")
  # Extract just the container name (first column, strip ansi and tab)
  _kfzf_extract_name "$result"
}

# Complete ports for a pod or service (uses cached data from server)
_kfzf_complete_ports() {
  local resource_name=$1
  local namespace=$2
  local context=$3
  local query=${4:-}
  local resource_type=${5:-pods}

  local -a kfzf_args=(ports "$resource_name" -t "$resource_type")
  [[ -n "$namespace" ]] && kfzf_args+=(-n "$namespace")
  [[ -n "$context" ]] && kfzf_args+=(-c "$context")

  local ports
  ports=$(kfzf "${kfzf_args[@]}" 2>/dev/null)

  if [[ -z "$ports" ]]; then
    return
  fi

  local current_ctx=$(_kfzf_current_context)
  local header="ctx: ${context:-$current_ctx}"
  [[ -n "$namespace" ]] && header="$header | ns: $namespace"
  if [[ "$resource_type" == "services" ]]; then
    header="$header | svc: $resource_name | ports (PORT TARGET PROTO NAME)"
  else
    header="$header | pod: $resource_name | ports (PORT PROTO CONTAINER NAME)"
  fi

  # Build preview command for service/pod info
  local ctx_arg=""
  [[ -n "$context" ]] && ctx_arg="--context $context"
  local ns_arg=""
  [[ -n "$namespace" ]] && ns_arg="-n $namespace"

  local preview_cmd
  if [[ "$resource_type" == "services" ]]; then
    # Service preview: show service details including selector, type, and endpoints
    preview_cmd="port=\$(echo {} | awk '{print \$1}'); echo '=== Service: $resource_name ==='; kubectl $ctx_arg get svc $ns_arg $resource_name -o wide 2>/dev/null; echo ''; echo '=== Service Details ==='; kubectl $ctx_arg get svc $ns_arg $resource_name -o jsonpath='{\"Type: \"}{.spec.type}{\"\nClusterIP: \"}{.spec.clusterIP}{\"\nSelector: \"}{.spec.selector}{\"\n\"}' 2>/dev/null; echo ''; echo '=== Endpoints ==='; kubectl $ctx_arg get endpoints $ns_arg $resource_name 2>/dev/null; echo ''; echo '=== Selected port ==='; echo \"Port: \$port -> Target: \$(echo {} | awk '{print \$2}')\""
  else
    # Pod preview: show pod info
    preview_cmd="port=\$(echo {} | awk '{print \$1}'); echo '=== Pod: $resource_name ==='; kubectl $ctx_arg get pod $ns_arg $resource_name -o wide 2>/dev/null; echo ''; echo '=== Selected port ==='; echo \"Port: \$port (Container: \$(echo {} | awk '{print \$3}'))\""
  fi

  local fzf_args=(
    --ansi
    --header="$header"
    --header-first
    --no-hscroll
    --tabstop=4
    --exit-0
    --border=rounded
    --prompt="port > "
    --pointer="▶ "
    --marker="★ "
    --color="header:italic:cyan,prompt:bold:blue,pointer:bold:magenta,marker:green"
    --layout=reverse
    --preview="$preview_cmd"
    --preview-window=right:50%:wrap
    --bind "ctrl-p:toggle-preview"
    --bind "tab:down,shift-tab:up"
    --select-1
  )
  [[ -n "$query" ]] && fzf_args+=(--query="^$query")

  local result
  result=$(echo "$ports" | fzf "${fzf_args[@]}" 2>/dev/tty)
  if [[ -n "$result" ]]; then
    # Extract just the port number (first column)
    echo "$result" | awk '{print $1}'
  fi
}

# Complete labels (uses cached data from server)
_kfzf_complete_labels() {
  local resource_type=$1
  local namespace=$2
  local context=$3
  local query=${4:-}

  local -a kfzf_args=(labels "$resource_type")
  [[ -n "$namespace" ]] && kfzf_args+=(-n "$namespace")
  [[ -n "$context" ]] && kfzf_args+=(-c "$context")

  local labels
  labels=$(kfzf "${kfzf_args[@]}" 2>/dev/null)

  if [[ -z "$labels" ]]; then
    return
  fi

  local current_ctx=$(_kfzf_current_context)
  local header="ctx: ${context:-$current_ctx}"
  [[ -n "$namespace" ]] && header="$header | ns: $namespace"
  header="$header | $resource_type labels"

  local result
  result=$(echo "$labels" | _kfzf_fzf "$header" "label > " "$query")
  [[ -n "$result" ]] && echo "$result"
}

# Complete field selectors (uses cached data from server)
_kfzf_complete_field_selector() {
  local resource_type=$1
  local namespace=$2
  local context=$3
  local query=${4:-}

  # If query contains =, we're completing a value for a specific field
  if [[ "$query" == *"="* ]]; then
    local field_name="${query%%=*}"
    local value_query="${query#*=}"

    local -a kfzf_args=(field-values "$resource_type" "$field_name")
    [[ -n "$namespace" ]] && kfzf_args+=(-n "$namespace")
    [[ -n "$context" ]] && kfzf_args+=(-c "$context")

    local values
    values=$(kfzf "${kfzf_args[@]}" 2>/dev/null)

    if [[ -z "$values" ]]; then
      return
    fi

    local current_ctx=$(_kfzf_current_context)
    local header="ctx: ${context:-$current_ctx}"
    [[ -n "$namespace" ]] && header="$header | ns: $namespace"
    header="$header | $resource_type field: $field_name"

    local result
    result=$(echo "$values" | _kfzf_fzf "$header" "value > " "$value_query")
    [[ -n "$result" ]] && echo "$result"
  else
    # Show available field names
    local fields="metadata.name
metadata.namespace
spec.nodeName
spec.restartPolicy
spec.schedulerName
spec.serviceAccountName
status.phase
status.podIP
status.nominatedNodeName"

    local current_ctx=$(_kfzf_current_context)
    local header="ctx: ${context:-$current_ctx} | $resource_type field selectors"

    local result
    result=$(echo "$fields" | _kfzf_fzf "$header" "field > " "$query")
    [[ -n "$result" ]] && echo "${result}="
  fi
}

# Complete files (for -f/--filename)
_kfzf_complete_file() {
  local query=${1:-}

  # Start from query directory or current directory
  local search_dir="."
  local file_query=""
  if [[ -n "$query" ]]; then
    if [[ -d "$query" ]]; then
      search_dir="$query"
      file_query=""
    elif [[ "$query" == */ ]]; then
      search_dir="$query"
      file_query=""
    elif [[ "$query" == */* ]]; then
      search_dir="${query%/*}"
      file_query="${query##*/}"
      [[ -z "$search_dir" ]] && search_dir="/"
    else
      file_query="$query"
    fi
  fi

  # Find YAML, JSON, and directories
  local files
  files=$(find "$search_dir" -maxdepth 3 \( -type f \( -name "*.yaml" -o -name "*.yml" -o -name "*.json" \) -o -type d \) 2>/dev/null | sort)

  if [[ -z "$files" ]]; then
    return
  fi

  local header="Select file (yaml/yml/json)"

  # Preview command for file contents
  local preview_cmd='[[ -d {} ]] && ls -la {} || head -50 {}'

  local fzf_args=(
    --ansi
    --header="$header"
    --header-first
    --no-hscroll
    --exit-0
    --border=rounded
    --prompt="file > "
    --pointer="▶ "
    --marker="★ "
    --color="header:italic:cyan,prompt:bold:blue,pointer:bold:magenta,marker:green"
    --layout=reverse
    --preview="$preview_cmd"
    --preview-window=right:50%:wrap
    --bind "ctrl-p:toggle-preview"
    --bind "tab:down,shift-tab:up"
  )
  [[ -n "$file_query" ]] && fzf_args+=(--query="$file_query")

  local result
  result=$(echo "$files" | fzf "${fzf_args[@]}" 2>/dev/tty)

  if [[ -n "$result" ]]; then
    # If directory selected, append / to allow drilling down
    if [[ -d "$result" ]]; then
      echo "${result}/"
    else
      echo "$result"
    fi
  fi
}

# Complete resource types (api-resources)
_kfzf_complete_resource_type() {
  local context=$1
  local query=${2:-}

  # Get api-resources with their short names
  local resources
  if [[ -n "$context" ]]; then
    resources=$(kubectl --context "$context" api-resources --verbs=list -o name 2>/dev/null | sort -u)
  else
    resources=$(kubectl api-resources --verbs=list -o name 2>/dev/null | sort -u)
  fi

  if [[ -z "$resources" ]]; then
    return
  fi

  local current_ctx=$(_kfzf_current_context)
  local header="ctx: ${context:-$current_ctx} | resource types"

  local result
  result=$(echo "$resources" | _kfzf_fzf "$header" "resource > " "$query")
  echo "$result"
}

# Main widget
_kfzf_kubectl_complete_widget() {
  local words=(${(z)LBUFFER})
  local cmd=${words[1]}
  local nwords=$#words

  if [[ "$cmd" != "kubectl" && "$cmd" != "k" ]]; then
    zle fzf-tab-complete
    return
  fi

  # Check if cursor is after a space (completing new word vs partial word)
  local completing_partial=0
  [[ "${LBUFFER[-1]}" != " " ]] && completing_partial=1

  local last_word=""
  local second_last=""
  if (( nwords >= 1 )); then
    last_word="${words[-1]}"
  fi
  if (( nwords >= 2 )); then
    second_last="${words[-2]}"
  fi

  # Parse the command line to extract all components
  local namespace=""
  local context=""
  local action=""
  local resource_type=""
  local resource_name=""
  local container=""
  local expecting=""  # What the next positional arg should be
  local all_namespaces=0  # Track if -A/--all-namespaces was used
  local svc_prefix=""  # Track svc/ or service/ prefix for port-forward
  local i=2

  # Flags that take a value
  local -A flag_values
  flag_values=(
    [-n]="namespace"
    [--namespace]="namespace"
    [--context]="context"
    [-c]="container"
    [--container]="container"
    [-l]="label"
    [--selector]="label"
    [-f]="file"
    [--filename]="file"
    [-o]="output"
    [--output]="output"
    [--field-selector]="field_selector"
  )

  # Boolean flags (no value)
  local -A bool_flags
  bool_flags=(
    [-A]=1 [--all-namespaces]=1
    [-w]=1 [--watch]=1
    [--all]=1 [--force]=1
    [-it]=1 [--stdin]=1 [--tty]=1
  )

  # Actions that have implicit resource type (pods)
  local -A implicit_pods
  implicit_pods=([logs]=1 [exec]=1 [attach]=1 [cp]=1 [port-forward]=1)

  # Known kubectl actions
  local -A known_actions
  known_actions=(
    [get]=1 [describe]=1 [delete]=1 [edit]=1 [apply]=1 [create]=1
    [logs]=1 [exec]=1 [attach]=1 [cp]=1
    [port-forward]=1 [scale]=1 [rollout]=1
    [label]=1 [annotate]=1 [patch]=1 [top]=1
    [run]=1 [expose]=1 [set]=1 [explain]=1
    [config]=1 [cluster-info]=1 [api-resources]=1 [api-versions]=1
    [diff]=1 [wait]=1 [auth]=1 [debug]=1 [events]=1
    [cnpg]=1
  )

  # Compound commands (like rollout, cnpg) that have subactions
  local -A compound_commands
  compound_commands=([rollout]=1 [cnpg]=1)

  # Valid rollout subactions
  local -A rollout_subactions
  rollout_subactions=([status]=1 [restart]=1 [undo]=1 [history]=1 [pause]=1 [resume]=1)

  # Valid cnpg subactions that expect a cluster resource
  local -A cnpg_subactions
  cnpg_subactions=([status]=1 [promote]=1 [restart]=1 [reload]=1 [maintenance]=1 [fencing]=1 [hibernate]=1 [destroy]=1 [logs]=1 [pgbench]=1 [fio]=1)

  # Track subaction for compound commands
  local subaction=""

  while (( i <= nwords )); do
    local word="${words[$i]}"
    local next_word=""
    (( i + 1 <= nwords )) && next_word="${words[$((i+1))]}"

    # Check if this is a flag that takes a value
    if [[ -n "${flag_values[$word]}" ]]; then
      local flag_type="${flag_values[$word]}"
      case "$flag_type" in
        namespace) namespace="$next_word" ;; 
        context) context="$next_word" ;; 
        container) container="$next_word" ;; 
      esac
      ((i+=2))
      continue
    fi

    # Check for --flag=value format
    if [[ "$word" == *=* ]]; then
      local flag_part="${word%%=*}"
      local value_part="${word#*=}"
      if [[ -n "${flag_values[$flag_part]}" ]]; then
        local flag_type="${flag_values[$flag_part]}"
        case "$flag_type" in
          namespace) namespace="$value_part" ;; 
          context) context="$value_part" ;; 
          container) container="$value_part" ;; 
        esac
      fi
      ((i++))
      continue
    fi

    # Check for boolean flags
    if [[ -n "${bool_flags[$word]}" ]]; then
      # Track -A/--all-namespaces
      if [[ "$word" == "-A" || "$word" == "--all-namespaces" ]]; then
        all_namespaces=1
      fi
      ((i++))
      continue
    fi

    # Skip other flags
    if [[ "$word" == -* ]]; then
      ((i++))
      continue
    fi

    # Positional arguments
    if [[ -z "$action" ]]; then
      if [[ -n "${known_actions[$word]}" ]]; then
        action="$word"
      fi
      ((i++))
      continue
    fi

    # For compound commands (like rollout, cnpg), the next positional arg is the subaction
    if [[ -n "${compound_commands[$action]}" && -z "$subaction" ]]; then
      # Check if it's a valid subaction for this command
      if [[ "$action" == "rollout" && -n "${rollout_subactions[$word]}" ]]; then
        subaction="$word"
      elif [[ "$action" == "cnpg" && -n "${cnpg_subactions[$word]}" ]]; then
        subaction="$word"
        # For cnpg, use full GVR to distinguish from Rancher clusters
        resource_type="clusters.postgresql.cnpg.io"
      elif (( i == nwords && completing_partial == 1 )); then
        # Completing partial subaction - don't set it
        :
      else
        # Unknown subaction - might be a partial match, skip it
        subaction="$word"
      fi
      ((i++))
      continue
    fi

    if [[ -z "$resource_type" ]]; then
      # For implicit pod commands, this is the resource name (pod)
      if [[ -n "${implicit_pods[$action]}" ]]; then
        # For port-forward, handle svc/ or service/ prefix (with or without service name)
        if [[ "$action" == "port-forward" && ("$word" == svc/* || "$word" == service/* || "$word" == "svc/" || "$word" == "service/") ]]; then
          resource_type="services"
          # Track the prefix used (svc/ or service/)
          if [[ "$word" == svc* ]]; then
            svc_prefix="svc/"
          else
            svc_prefix="service/"
          fi
          resource_name="${word#*/}"
          # If resource_name is empty or we're completing partial, don't set it
          if [[ -z "$resource_name" ]] || (( i == nwords && completing_partial == 1 )); then
            complete_query="$resource_name"
            resource_name=""
            ((i++))
            continue
          fi
        else
          resource_type="pods"
          # Check if this is the last word and we're completing partial
          # If so, this might be a partial pod name, not a complete one
          if (( i == nwords && completing_partial == 1 )); then
            # Check for partial svc/ or service/ prefix for port-forward
            if [[ "$action" == "port-forward" && ("$word" == svc/* || "$word" == service/* || "$word" == "svc/" || "$word" == "service/") ]]; then
              resource_type="services"
              complete_query="${word#*/}"
            fi
            # Don't set resource_name - let it be completed as resource
            ((i++))
            continue
          fi
          resource_name="$word"
        fi
      else
        # Check if this is the last word and we're completing partial
        # If so, this might be a partial resource type, not a complete one
        if (( i == nwords && completing_partial == 1 )); then
          # Don't set resource_type - let it be completed as resource_type
          ((i++))
          continue
        fi
        resource_type="$word"
      fi
      ((i++))
      continue
    fi

    if [[ -z "$resource_name" ]]; then
      resource_name="$word"
      ((i++))
      continue
    fi

    ((i++))
  done

  # Determine what we're completing based on cursor position
  local complete_type=""
  local complete_query=""

  # Check if we're completing a flag value
  if (( completing_partial == 0 )); then
    # Cursor after space - check what the last complete word is
    case "$last_word" in
      -n|--namespace) 
        complete_type="namespace"
        ;;;
      --context)
        complete_type="context"
        ;;;
      -c|--container)
        complete_type="container"
        ;;;
      -l|--selector)
        complete_type="label"
        ;;;
      --field-selector)
        complete_type="field_selector"
        ;;;
      -f|--filename)
        complete_type="file"
        ;;;
    esac
  else
    # Cursor in middle of word - check if last word is a flag starting with --
    # If so, fall back to standard completion for flag names
    if [[ "$last_word" == --* && "$last_word" != *=* ]]; then
      zle fzf-tab-complete
      return
    fi
    # Cursor in middle of word - check second_last
    case "$second_last" in
      -n|--namespace) 
        complete_type="namespace"
        complete_query="$last_word"
        ;;;
      --context)
        complete_type="context"
        complete_query="$last_word"
        ;;;
      -c|--container)
        complete_type="container"
        complete_query="$last_word"
        ;;;
      -l|--selector)
        complete_type="label"
        complete_query="$last_word"
        ;;;
      --field-selector)
        complete_type="field_selector"
        complete_query="$last_word"
        ;;;
      -f|--filename)
        complete_type="file"
        complete_query="$last_word"
        ;;;
    esac
  fi

  # Set implicit resource type for pod commands (before checking complete_type)
  if [[ -z "$resource_type" && -n "${implicit_pods[$action]}" ]]; then
    resource_type="pods"
  fi

  # If not completing a flag value, determine based on position
  if [[ -z "$complete_type" ]]; then
    if [[ -z "$action" ]]; then
      # No action yet - fall back to standard completion
      zle fzf-tab-complete
      return
    fi

    # For compound commands, check if we need to complete subaction first
    if [[ -n "${compound_commands[$action]}" && -z "$subaction" ]]; then
      # Need to complete subaction (e.g., rollout status/restart/undo)
      # Fall back to standard completion for now - kubectl handles subaction completion
      zle fzf-tab-complete
      return
    fi

    if [[ -z "$resource_type" ]]; then
      # Need to complete resource type
      complete_type="resource_type"
      if (( completing_partial == 1 )); then
        # Check if last_word looks like a partial resource type (not a flag)
        if [[ "$last_word" != -* ]]; then
          complete_query="$last_word"
        fi
      fi
    elif [[ -n "$resource_name" && -n "${implicit_pods[$action]}" ]]; then
      # For logs/exec/attach: if we already have a pod name, complete container
      # For port-forward: complete ports instead
      if [[ "$action" == "port-forward" ]]; then
        complete_type="port"
      else
        complete_type="container"
      fi
      if (( completing_partial == 1 )); then
        if [[ "$last_word" != -* && "$last_word" != "$resource_name" ]]; then
          complete_query="$last_word"
        fi
      fi
    else
      # We have action and resource_type - complete resource name
      complete_type="resource"
      if (( completing_partial == 1 )); then
        # Check if last_word looks like a partial resource name (not a flag)
        if [[ "$last_word" != -* && -n "$action" ]]; then
          # For svc/ or service/ prefix, extract just the part after the slash
          if [[ -n "$svc_prefix" ]]; then
            complete_query="${last_word#*/}"
          else
            complete_query="$last_word"
          fi
        fi
      fi
    fi
  fi

  # If no explicit --context was provided, use the shell's current context
  # This enables per-shell context isolation (each shell can have different KUBECONFIG)
  if [[ -z "$context" ]]; then
    # Strip ANSI color codes that kubectx or other tools might add
    context=$(_kfzf_current_context)
  fi

  # Check if server is running (for resource/namespace completion)
  if [[ "$complete_type" == "resource" || "$complete_type" == "namespace" || "$complete_type" == "label" ]]; then
    if ! kfzf status &>/dev/null; then
      zle fzf-tab-complete
      return
    fi
  fi

  # Do the completion
  local result=""
  case "$complete_type" in
    namespace)
      result=$(_kfzf_complete_namespace "$complete_query" "$context")
      ;;;
    context)
      result=$(_kfzf_complete_context "$complete_query")
      ;;;
    container)
      if [[ -n "$resource_name" ]]; then
        result=$(_kfzf_complete_container "$resource_name" "$namespace" "$context" "$complete_query")
      fi
      ;;;
    port)
      if [[ -n "$resource_name" ]]; then
        result=$(_kfzf_complete_ports "$resource_name" "$namespace" "$context" "$complete_query" "$resource_type")
      fi
      ;;;
    label)
      if [[ -n "$resource_type" ]]; then
        result=$(_kfzf_complete_labels "$resource_type" "$namespace" "$context" "$complete_query")
      fi
      ;;;
    field_selector)
      if [[ -n "$resource_type" ]]; then
        result=$(_kfzf_complete_field_selector "$resource_type" "$namespace" "$context" "$complete_query")
      fi
      ;;;
    file)
      result=$(_kfzf_complete_file "$complete_query")
      ;;;
    resource_type)
      result=$(_kfzf_complete_resource_type "$context" "$complete_query")
      ;;;
    resource)
      result=$(_kfzf_complete_resource "$resource_type" "$namespace" "$context" "$complete_query" "$all_namespaces")
      ;;;
  esac

  if [[ -n "$result" ]]; then
    # Handle -A mode: result is "ns:name" format, need to replace -A with -n ns name
    if [[ "$all_namespaces" == "1" && "$complete_type" == "resource" && -z "$namespace" ]]; then
      # Parse ns:name pairs and build new command
      local new_parts=()
      for item in $result; do
        if [[ "$item" == *":"* ]]; then
          local ns="${item%%:*}"
          local name="${item#*:}"
          new_parts+=("-n" "$ns" "$name")
        else
          new_parts+=("$item")
        fi
      done
      # Remove -A or --all-namespaces from LBUFFER and append -n ns name
      LBUFFER="${LBUFFER//-A /}"
      LBUFFER="${LBUFFER//--all-namespaces /}"
      # Remove trailing spaces and partial query
      LBUFFER="${LBUFFER%% }"
      [[ -n "$complete_query" ]] && LBUFFER="${LBUFFER%$complete_query}"
      LBUFFER="${LBUFFER%% }"
      # Append the new parts
      LBUFFER="${LBUFFER} ${new_parts[*]} "
    else
      # Handle svc/ or service/ prefix for port-forward
      if [[ -n "$svc_prefix" && "$complete_type" == "resource" ]]; then
        # Prepend the svc/ or service/ prefix to the result
        result="${svc_prefix}${result}"
        # Remove the existing svc/ or service/ prefix (with any partial query) from LBUFFER
        LBUFFER="${LBUFFER%${svc_prefix}${complete_query}}${result}"
      elif [[ -n "$complete_query" ]]; then
        # Replace partial word
        LBUFFER="${LBUFFER%$complete_query}${result}"
      else
        # Append new word
        LBUFFER="${LBUFFER}${result}"
      fi
      # Add trailing space for convenience
      [[ "${LBUFFER[-1]}" != " " ]] && LBUFFER="${LBUFFER} "
    fi
    zle redisplay
  fi
}

# Create the widget
zle -N _kfzf_kubectl_complete_widget

# Tab triggers kfzf for kubectl, normal completion for everything else
_kfzf_tab_complete() {
  local words=(${(z)LBUFFER})
  local cmd=${words[1]}

  if [[ "$cmd" == "kubectl" || "$cmd" == "k" ]]; then
    _kfzf_kubectl_complete_widget
  else
    zle fzf-tab-complete
  fi
}
zle -N _kfzf_tab_complete
bindkey '^I' _kfzf_tab_complete