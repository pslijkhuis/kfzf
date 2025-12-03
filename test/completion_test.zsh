#!/usr/bin/env zsh
# Integration tests for kfzf ZSH completion parsing logic
# Run with: zsh test/completion_test.zsh
#
# These tests verify the command-line parsing logic that determines
# what type of completion should be offered.

# Don't use set -e because arithmetic operations can return non-zero

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m' # No Color

PASS=0
FAIL=0

# Test helper
assert_eq() {
  local name="$1"
  local expected="$2"
  local actual="$3"

  if [[ "$expected" == "$actual" ]]; then
    echo "${GREEN}PASS${NC}: $name"
    ((PASS++))
  else
    echo "${RED}FAIL${NC}: $name"
    echo "  Expected: '$expected'"
    echo "  Actual:   '$actual'"
    ((FAIL++))
  fi
}

# Test parsing function - this is the same logic as in the ZSH completion widget
_test_parse_cmdline() {
  local cmdline="$1"
  local words=(${(z)cmdline})
  local cmd=${words[1]}
  local nwords=$#words

  if [[ "$cmd" != "kubectl" && "$cmd" != "k" ]]; then
    echo "not_kubectl"
    return
  fi

  local completing_partial=0
  [[ "${cmdline[-1]}" != " " ]] && completing_partial=1

  local last_word=""
  local second_last=""
  if (( nwords >= 1 )); then
    last_word="${words[-1]}"
  fi
  if (( nwords >= 2 )); then
    second_last="${words[-2]}"
  fi

  local namespace=""
  local context=""
  local action=""
  local resource_type=""
  local resource_name=""
  local all_namespaces=0
  local svc_prefix=""
  local i=2

  local -A flag_values
  flag_values=(
    [-n]="namespace"
    [--namespace]="namespace"
    [--context]="context"
    [-c]="container"
    [--container]="container"
    [-l]="label"
    [--selector]="label"
    [--field-selector]="field_selector"
  )

  local -A bool_flags
  bool_flags=(
    [-A]=1 [--all-namespaces]=1
  )

  local -A implicit_pods
  implicit_pods=([logs]=1 [exec]=1 [attach]=1 [cp]=1 [port-forward]=1)

  local -A known_actions
  known_actions=(
    [get]=1 [describe]=1 [delete]=1 [edit]=1
    [logs]=1 [exec]=1 [attach]=1 [port-forward]=1
    [rollout]=1
  )

  # Compound commands (like rollout) that have subactions
  local -A compound_commands
  compound_commands=([rollout]=1)

  # Valid rollout subactions
  local -A rollout_subactions
  rollout_subactions=([status]=1 [restart]=1 [undo]=1 [history]=1 [pause]=1 [resume]=1)

  # Track subaction for compound commands
  local subaction=""

  while (( i <= nwords )); do
    local word="${words[$i]}"
    local next_word=""
    (( i + 1 <= nwords )) && next_word="${words[$((i+1))]}"

    if [[ -n "${flag_values[$word]}" ]]; then
      local flag_type="${flag_values[$word]}"
      case "$flag_type" in
        namespace) namespace="$next_word" ;;
        context) context="$next_word" ;;
      esac
      ((i+=2))
      continue
    fi

    if [[ "$word" == *=* ]]; then
      local flag_part="${word%%=*}"
      local value_part="${word#*=}"
      if [[ -n "${flag_values[$flag_part]}" ]]; then
        local flag_type="${flag_values[$flag_part]}"
        case "$flag_type" in
          namespace) namespace="$value_part" ;;
          context) context="$value_part" ;;
        esac
      fi
      ((i++))
      continue
    fi

    if [[ -n "${bool_flags[$word]}" ]]; then
      if [[ "$word" == "-A" || "$word" == "--all-namespaces" ]]; then
        all_namespaces=1
      fi
      ((i++))
      continue
    fi

    if [[ "$word" == -* ]]; then
      ((i++))
      continue
    fi

    if [[ -z "$action" ]]; then
      if [[ -n "${known_actions[$word]}" ]]; then
        action="$word"
      fi
      ((i++))
      continue
    fi

    # For compound commands (like rollout), the next positional arg is the subaction
    if [[ -n "${compound_commands[$action]}" && -z "$subaction" ]]; then
      # For rollout, check if it's a valid subaction
      if [[ "$action" == "rollout" && -n "${rollout_subactions[$word]}" ]]; then
        subaction="$word"
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
      if [[ -n "${implicit_pods[$action]}" ]]; then
        if [[ "$action" == "port-forward" && ("$word" == svc/* || "$word" == service/* || "$word" == "svc/" || "$word" == "service/") ]]; then
          resource_type="services"
          if [[ "$word" == svc* ]]; then
            svc_prefix="svc/"
          else
            svc_prefix="service/"
          fi
          resource_name="${word#*/}"
          if [[ -z "$resource_name" ]] || (( i == nwords && completing_partial == 1 )); then
            resource_name=""
            ((i++))
            continue
          fi
        else
          resource_type="pods"
          if (( i == nwords && completing_partial == 1 )); then
            ((i++))
            continue
          fi
          resource_name="$word"
        fi
      else
        if (( i == nwords && completing_partial == 1 )); then
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

  # Determine complete_type
  local complete_type=""
  local complete_query=""

  if (( completing_partial == 0 )); then
    case "$last_word" in
      -n|--namespace) complete_type="namespace" ;;
      --context) complete_type="context" ;;
      -c|--container) complete_type="container" ;;
      -l|--selector) complete_type="label" ;;
      --field-selector) complete_type="field_selector" ;;
    esac
  else
    case "$second_last" in
      -n|--namespace) complete_type="namespace"; complete_query="$last_word" ;;
      --context) complete_type="context"; complete_query="$last_word" ;;
      -c|--container) complete_type="container"; complete_query="$last_word" ;;
      -l|--selector) complete_type="label"; complete_query="$last_word" ;;
      --field-selector) complete_type="field_selector"; complete_query="$last_word" ;;
    esac
  fi

  if [[ -z "$resource_type" && -n "${implicit_pods[$action]}" ]]; then
    resource_type="pods"
  fi

  if [[ -z "$complete_type" ]]; then
    if [[ -z "$action" ]]; then
      complete_type="action"
    elif [[ -n "${compound_commands[$action]}" && -z "$subaction" ]]; then
      # For compound commands without subaction, complete subaction
      complete_type="subaction"
    elif [[ -z "$resource_type" ]]; then
      complete_type="resource_type"
    elif [[ -n "$resource_name" && -n "${implicit_pods[$action]}" ]]; then
      if [[ "$action" == "port-forward" ]]; then
        complete_type="port"
      else
        complete_type="container"
      fi
    else
      complete_type="resource"
      if (( completing_partial == 1 )); then
        if [[ "$last_word" != -* && -n "$action" ]]; then
          if [[ -n "$svc_prefix" ]]; then
            complete_query="${last_word#*/}"
          else
            complete_query="$last_word"
          fi
        fi
      fi
    fi
  fi

  # Output parsed values
  echo "action=$action"
  echo "subaction=$subaction"
  echo "resource_type=$resource_type"
  echo "resource_name=$resource_name"
  echo "namespace=$namespace"
  echo "context=$context"
  echo "all_namespaces=$all_namespaces"
  echo "svc_prefix=$svc_prefix"
  echo "complete_type=$complete_type"
  echo "complete_query=$complete_query"
}

# Helper to get specific field from parse output
_get_field() {
  local output="$1"
  local field="$2"
  echo "$output" | grep "^${field}=" | cut -d= -f2
}

echo "=== kfzf ZSH Completion Integration Tests ==="
echo ""

# Test: kubectl get -n <tab>
result=$(_test_parse_cmdline "kubectl get -n ")
assert_eq "kubectl get -n <tab> -> complete namespace" "namespace" "$(_get_field "$result" "complete_type")"

# Test: kubectl get --namespace <tab>
result=$(_test_parse_cmdline "kubectl get --namespace ")
assert_eq "kubectl get --namespace <tab> -> complete namespace" "namespace" "$(_get_field "$result" "complete_type")"

# Test: kubectl get -n def (partial)
result=$(_test_parse_cmdline "kubectl get -n def")
assert_eq "kubectl get -n def<tab> -> complete namespace" "namespace" "$(_get_field "$result" "complete_type")"
assert_eq "kubectl get -n def<tab> -> query=def" "def" "$(_get_field "$result" "complete_query")"

# Test: kubectl get <tab>
result=$(_test_parse_cmdline "kubectl get ")
assert_eq "kubectl get <tab> -> complete resource_type" "resource_type" "$(_get_field "$result" "complete_type")"

# Test: kubectl get pods <tab>
result=$(_test_parse_cmdline "kubectl get pods ")
assert_eq "kubectl get pods <tab> -> complete resource" "resource" "$(_get_field "$result" "complete_type")"
assert_eq "kubectl get pods <tab> -> resource_type=pods" "pods" "$(_get_field "$result" "resource_type")"

# Test: kubectl get pods -l <tab>
result=$(_test_parse_cmdline "kubectl get pods -l ")
assert_eq "kubectl get pods -l <tab> -> complete label" "label" "$(_get_field "$result" "complete_type")"
assert_eq "kubectl get pods -l <tab> -> resource_type=pods" "pods" "$(_get_field "$result" "resource_type")"

# Test: kubectl get pods --field-selector <tab>
result=$(_test_parse_cmdline "kubectl get pods --field-selector ")
assert_eq "kubectl get pods --field-selector <tab> -> complete field_selector" "field_selector" "$(_get_field "$result" "complete_type")"

# Test: kubectl logs <tab>
result=$(_test_parse_cmdline "kubectl logs ")
assert_eq "kubectl logs <tab> -> complete resource" "resource" "$(_get_field "$result" "complete_type")"
assert_eq "kubectl logs <tab> -> resource_type=pods" "pods" "$(_get_field "$result" "resource_type")"

# Test: kubectl logs my-pod <tab>
result=$(_test_parse_cmdline "kubectl logs my-pod ")
assert_eq "kubectl logs my-pod <tab> -> complete container" "container" "$(_get_field "$result" "complete_type")"
assert_eq "kubectl logs my-pod <tab> -> resource_name=my-pod" "my-pod" "$(_get_field "$result" "resource_name")"

# Test: kubectl logs my-pod -c <tab>
result=$(_test_parse_cmdline "kubectl logs my-pod -c ")
assert_eq "kubectl logs my-pod -c <tab> -> complete container" "container" "$(_get_field "$result" "complete_type")"

# Test: kubectl port-forward <tab>
result=$(_test_parse_cmdline "kubectl port-forward ")
assert_eq "kubectl port-forward <tab> -> complete resource" "resource" "$(_get_field "$result" "complete_type")"
assert_eq "kubectl port-forward <tab> -> resource_type=pods" "pods" "$(_get_field "$result" "resource_type")"

# Test: kubectl port-forward my-pod <tab>
result=$(_test_parse_cmdline "kubectl port-forward my-pod ")
assert_eq "kubectl port-forward my-pod <tab> -> complete port" "port" "$(_get_field "$result" "complete_type")"

# Test: kubectl port-forward svc/my-svc <tab>
result=$(_test_parse_cmdline "kubectl port-forward svc/my-svc ")
assert_eq "kubectl port-forward svc/my-svc <tab> -> complete port" "port" "$(_get_field "$result" "complete_type")"
assert_eq "kubectl port-forward svc/my-svc <tab> -> resource_type=services" "services" "$(_get_field "$result" "resource_type")"
assert_eq "kubectl port-forward svc/my-svc <tab> -> resource_name=my-svc" "my-svc" "$(_get_field "$result" "resource_name")"

# Test: kubectl port-forward -n kube-system svc/ (empty after svc/)
result=$(_test_parse_cmdline "kubectl port-forward -n kube-system svc/")
assert_eq "kubectl port-forward -n kube-system svc/<tab> -> complete resource" "resource" "$(_get_field "$result" "complete_type")"
assert_eq "kubectl port-forward -n kube-system svc/<tab> -> resource_type=services" "services" "$(_get_field "$result" "resource_type")"
assert_eq "kubectl port-forward -n kube-system svc/<tab> -> namespace=kube-system" "kube-system" "$(_get_field "$result" "namespace")"
assert_eq "kubectl port-forward -n kube-system svc/<tab> -> svc_prefix=svc/" "svc/" "$(_get_field "$result" "svc_prefix")"

# Test: kubectl port-forward -n default svc/core (partial service name)
result=$(_test_parse_cmdline "kubectl port-forward -n default svc/core")
assert_eq "kubectl port-forward -n default svc/core<tab> -> complete resource" "resource" "$(_get_field "$result" "complete_type")"
assert_eq "kubectl port-forward -n default svc/core<tab> -> resource_type=services" "services" "$(_get_field "$result" "resource_type")"
assert_eq "kubectl port-forward -n default svc/core<tab> -> complete_query=core" "core" "$(_get_field "$result" "complete_query")"
assert_eq "kubectl port-forward -n default svc/core<tab> -> svc_prefix=svc/" "svc/" "$(_get_field "$result" "svc_prefix")"

# Test: kubectl port-forward service/my-service <tab>
result=$(_test_parse_cmdline "kubectl port-forward service/my-service ")
assert_eq "kubectl port-forward service/my-service <tab> -> complete port" "port" "$(_get_field "$result" "complete_type")"
assert_eq "kubectl port-forward service/my-service <tab> -> svc_prefix=service/" "service/" "$(_get_field "$result" "svc_prefix")"

# Test: kubectl --context <tab>
result=$(_test_parse_cmdline "kubectl --context ")
assert_eq "kubectl --context <tab> -> complete context" "context" "$(_get_field "$result" "complete_type")"

# Test: kubectl get pods -A <tab>
result=$(_test_parse_cmdline "kubectl get pods -A ")
assert_eq "kubectl get pods -A <tab> -> all_namespaces=1" "1" "$(_get_field "$result" "all_namespaces")"
assert_eq "kubectl get pods -A <tab> -> complete resource" "resource" "$(_get_field "$result" "complete_type")"

# Test: kubectl get pods -n kube-system --context prod <tab>
result=$(_test_parse_cmdline "kubectl get pods -n kube-system --context prod ")
assert_eq "kubectl get pods -n kube-system --context prod <tab> -> namespace=kube-system" "kube-system" "$(_get_field "$result" "namespace")"
assert_eq "kubectl get pods -n kube-system --context prod <tab> -> context=prod" "prod" "$(_get_field "$result" "context")"

# Test: k get pods <tab> (alias)
result=$(_test_parse_cmdline "k get pods ")
assert_eq "k get pods <tab> -> complete resource" "resource" "$(_get_field "$result" "complete_type")"

# Test: kubectl get namespaces <tab>
result=$(_test_parse_cmdline "kubectl get namespaces ")
assert_eq "kubectl get namespaces <tab> -> resource_type=namespaces" "namespaces" "$(_get_field "$result" "resource_type")"

# Test: kubectl rollout <tab> - complete subaction
result=$(_test_parse_cmdline "kubectl rollout ")
assert_eq "kubectl rollout <tab> -> complete subaction" "subaction" "$(_get_field "$result" "complete_type")"
assert_eq "kubectl rollout <tab> -> action=rollout" "rollout" "$(_get_field "$result" "action")"

# Test: kubectl rollout status <tab> - complete resource_type
result=$(_test_parse_cmdline "kubectl rollout status ")
assert_eq "kubectl rollout status <tab> -> complete resource_type" "resource_type" "$(_get_field "$result" "complete_type")"
assert_eq "kubectl rollout status <tab> -> action=rollout" "rollout" "$(_get_field "$result" "action")"
assert_eq "kubectl rollout status <tab> -> subaction=status" "status" "$(_get_field "$result" "subaction")"

# Test: kubectl rollout restart deployment <tab>
result=$(_test_parse_cmdline "kubectl rollout restart deployment ")
assert_eq "kubectl rollout restart deployment <tab> -> complete resource" "resource" "$(_get_field "$result" "complete_type")"
assert_eq "kubectl rollout restart deployment <tab> -> resource_type=deployment" "deployment" "$(_get_field "$result" "resource_type")"
assert_eq "kubectl rollout restart deployment <tab> -> subaction=restart" "restart" "$(_get_field "$result" "subaction")"

# Test: kubectl rollout undo deployment nginx <tab> - should not complete further
result=$(_test_parse_cmdline "kubectl rollout undo deployment nginx ")
assert_eq "kubectl rollout undo deployment nginx <tab> -> resource_name=nginx" "nginx" "$(_get_field "$result" "resource_name")"
assert_eq "kubectl rollout undo deployment nginx <tab> -> subaction=undo" "undo" "$(_get_field "$result" "subaction")"

# Summary
echo ""
echo "=== Summary ==="
echo "${GREEN}Passed: $PASS${NC}"
echo "${RED}Failed: $FAIL${NC}"

if (( FAIL > 0 )); then
  exit 1
fi
