#!/usr/bin/env bash
set -euo pipefail

SKILL_PATH="${1:-.}"
UPLOAD="${2:-false}"

ARGS=("regress" "$SKILL_PATH")
if [[ "$UPLOAD" == "true" ]]; then
  ARGS+=("--upload")
fi

"$(go env GOPATH)/bin/skillci" "${ARGS[@]}"

# Commit the badge if it changed, so the "shareable README badge" value prop
# actually reaches the repo instead of being regenerated and discarded on
# every run. This only commits locally within the checkout — it does NOT
# push. Pushing from a composite action inside arbitrary consumer workflows
# is a scope/permissions decision outside this action; the consuming
# workflow's own push step (if any) carries this commit forward.
# This convenience step is best-effort: it must never fail the action or
# mask the exit code of the skillci regress command above.
BADGE_PATH="$SKILL_PATH/.skillci/badge.svg"
if [[ -f "$BADGE_PATH" ]] && git -C "$SKILL_PATH" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  git -C "$SKILL_PATH" add .skillci/badge.svg || true
  git -C "$SKILL_PATH" diff --staged --quiet -- .skillci/badge.svg || git -C "$SKILL_PATH" commit -m "chore: update skillci badge" || true
fi
