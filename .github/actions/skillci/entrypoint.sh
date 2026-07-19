#!/usr/bin/env bash
set -euo pipefail

SKILL_PATH="${1:-.}"
UPLOAD="${2:-false}"

ARGS=("regress" "$SKILL_PATH")
if [[ "$UPLOAD" == "true" ]]; then
  ARGS+=("--upload")
fi

"$(go env GOPATH)/bin/skillci" "${ARGS[@]}"
