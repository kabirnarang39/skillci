#!/usr/bin/env bash
set -euo pipefail

SKILL_PATH="${1:-.}"
UPLOAD="${2:-false}"

ARGS=("regress" "$SKILL_PATH")
if [[ "$UPLOAD" == "true" ]]; then
  ARGS+=("--upload")
fi

# Capture the exit code explicitly instead of running the bare command
# under `set -e` — `skillci regress` exits non-zero on every caught
# regression, which is the NORMAL, expected outcome this whole action
# exists to surface. Under plain `set -e` that non-zero exit aborts the
# script immediately, silently skipping the state-commit block below on
# every run where it matters most.
regress_exit=0
"$(go env GOPATH)/bin/skillci" "${ARGS[@]}" || regress_exit=$?

# Commit skillci's own state (badge + history) if either changed, so both
# the "shareable README badge" value prop AND the self-growing eval loop's
# dedup + `skillci bisect`'s auto good/bad-SHA detection actually persist
# across runs, instead of being regenerated and discarded every time.
# Without history.json surviving between runs, `skillci regress` has no
# record of a previously-proposed generated case (re-proposing it every
# run) and `skillci bisect` has nothing to auto-detect endpoints from.
# This only commits locally within the checkout — it does NOT push.
# Pushing from a composite action inside arbitrary consumer workflows is a
# scope/permissions decision outside this action; the consuming workflow's
# own push step (if any) carries this commit forward.
# This convenience step is best-effort: it must never fail the action or
# mask the exit code of the skillci regress command above — hence running
# unconditionally here, and re-exiting with regress_exit at the end.
STATE_PATHS=(".skillci/badge.svg" ".skillci/history.json")
if git -C "$SKILL_PATH" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  for p in "${STATE_PATHS[@]}"; do
    [[ -f "$SKILL_PATH/$p" ]] && git -C "$SKILL_PATH" add "$p" || true
  done
  git -C "$SKILL_PATH" diff --staged --quiet -- "${STATE_PATHS[@]}" || git -C "$SKILL_PATH" commit -m "chore: update skillci badge and history" || true
fi

exit "$regress_exit"
