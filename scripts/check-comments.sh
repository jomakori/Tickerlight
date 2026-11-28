#!/usr/bin/env bash
# Fails on ticket/PR ids in comments, and on narration comments in config/CI files.
set -euo pipefail

fail=0

ticket_pattern='^[[:space:]]*(//|///|//!|/\*|\*|#)[^[:alnum:]]*.*(\b(OKT|OKA|PLY|JIRA)-[0-9]{1,5}\b|\b(PR|GH|issue|closes|fixes) ?#[0-9]{1,5})'
if hits=$(grep -nHE "$ticket_pattern" "$@" 2>/dev/null); then
  printf '%s\n' "$hits" >&2
  echo "comment: ticket/PR ids belong in the PR body, not in source" >&2
  fail=1
fi

config_shape='(^|/)\.github/|(^|/)\.pre-commit-config\.yaml$|(^|/)Tiltfile$|(^|/)Dockerfile[^/]*$|(^|/)\.yamllint[^/]*$|(^|/)\.gitleaks\.toml$|(^|/)renovate\.json$'
config_files=()
for f in "$@"; do
  [[ "$f" =~ $config_shape ]] && config_files+=("$f")
done

if [ "${#config_files[@]}" -gt 0 ]; then
  allowed='^[^:]+:[0-9]+:[[:space:]]*#!'
  allowed+='|^[^:]+:[0-9]+:[[:space:]]*#[[:space:]]*syntax='
  allowed+='|^[^:]+:[0-9]+:[[:space:]]*#[[:space:]]*(yamllint|shellcheck|actionlint:|pre-commit:|templ:)'
  allowed+='|^[^:]+:[0-9]+:[[:space:]]*#[[:space:]]*allow:'
  if hits=$(grep -nHE '^[[:space:]]*#' "${config_files[@]}" 2>/dev/null | grep -vE "$allowed"); then
    printf '%s\n' "$hits" >&2
    echo "comment: config/CI files carry no narration — only a shebang, a tool directive, or one 'allow:' line for a real constraint" >&2
    fail=1
  fi
fi

exit "$fail"
