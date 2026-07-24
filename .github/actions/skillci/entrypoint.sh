#!/usr/bin/env bash
set -euo pipefail

SKILL_PATH="${1:-.}"
UPLOAD="${2:-false}"
PR_COMMENT="${3:-false}"

ARGS=("regress" "$SKILL_PATH")
if [[ "$UPLOAD" == "true" ]]; then
  ARGS+=("--upload")
fi

REGRESS_OUTPUT_FILE="$(mktemp)"

# Capture the exit code explicitly instead of running the bare command
# under `set -e` — `skillci regress` exits non-zero on every caught
# regression, which is the NORMAL, expected outcome this whole action
# exists to surface. Under plain `set -e` that non-zero exit aborts the
# script immediately, silently skipping the state-commit block below on
# every run where it matters most.
#
# Piped through tee (rather than a plain `||` capture) so the PR-comment
# block below has the exact output to quote, while the run's own log
# still shows it live. `set +e` (not a trailing `|| true`) neutralizes
# errexit for just this line — appending `|| true` directly would run
# `true` as its own trivial pipeline the moment skillci fails, which
# overwrites PIPESTATUS with `true`'s own status (0) before the next
# line can read it, silently losing skillci's real exit code every time
# it matters. PIPESTATUS must be read immediately after the pipeline,
# with nothing else executed in between.
set +e
"$(go env GOPATH)/bin/skillci" "${ARGS[@]}" 2>&1 | tee "$REGRESS_OUTPUT_FILE"
regress_exit="${PIPESTATUS[0]}"
set -e

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

# post_or_update_pr_comment finds an existing skillci comment on the PR
# (identified by a hidden HTML marker in its body) and PATCHes it instead
# of POSTing a new one on every re-run — otherwise every push to the PR
# would pile up a fresh comment, the exact spam this pattern exists to
# avoid (the same technique Danger/promptfoo-action use).
post_or_update_pr_comment() {
  local repo="$1" pr_number="$2" body="$3" token="$4"
  local marker="<!-- skillci-pr-comment -->"
  local full_body
  full_body="$(printf '%s\n\n%s\n' "$body" "$marker")"

  local payload
  payload="$(jq -n --arg body "$full_body" '{body: $body}')"

  local existing_id
  existing_id="$(curl -sS -H "Authorization: Bearer $token" -H "Accept: application/vnd.github+json" \
    "https://api.github.com/repos/${repo}/issues/${pr_number}/comments?per_page=100" \
    | jq -r --arg marker "$marker" '[.[] | select(.body | contains($marker))] | first | .id // empty')"

  local url method
  if [[ -n "$existing_id" ]]; then
    url="https://api.github.com/repos/${repo}/issues/comments/${existing_id}"
    method="PATCH"
  else
    url="https://api.github.com/repos/${repo}/issues/${pr_number}/comments"
    method="POST"
  fi

  local response_file
  response_file="$(mktemp)"
  local http_status
  http_status="$(curl -sS -o "$response_file" -w '%{http_code}' \
    -X "$method" -H "Authorization: Bearer $token" -H "Accept: application/vnd.github+json" \
    -d "$payload" "$url")"

  if [[ "$http_status" -lt 200 || "$http_status" -ge 300 ]]; then
    echo "warning: PR comment ${method} failed with HTTP ${http_status}: $(cat "$response_file")" >&2
    return 1
  fi
}

if [[ "$PR_COMMENT" == "true" ]]; then
  if [[ "${GITHUB_EVENT_NAME:-}" == "pull_request" && -n "${GITHUB_EVENT_PATH:-}" && -f "${GITHUB_EVENT_PATH:-}" ]]; then
    pr_number="$(jq -r '.number // empty' "$GITHUB_EVENT_PATH")"
    if [[ -n "$pr_number" && -n "${GITHUB_TOKEN:-}" && -n "${GITHUB_REPOSITORY:-}" ]]; then
      pass_count="$(grep -c '^\[PASS\]' "$REGRESS_OUTPUT_FILE" || true)"
      fail_count="$(grep -c '^\[FAIL\]' "$REGRESS_OUTPUT_FILE" || true)"
      regressed_count="$(grep -c '^\[REGRESSED\]' "$REGRESS_OUTPUT_FILE" || true)"
      summary="**${pass_count} passed · ${fail_count} failed · ${regressed_count} regressed**"
      comment_body="$(printf '### 🧪 SkillCI Results\n\n%s\n\n<details>\n<summary>Full output</summary>\n\n```\n%s\n```\n\n</details>' \
        "$summary" "$(cat "$REGRESS_OUTPUT_FILE")")"
      post_or_update_pr_comment "$GITHUB_REPOSITORY" "$pr_number" "$comment_body" "$GITHUB_TOKEN" \
        || echo "warning: failed to post PR comment (continuing — this never fails the action)" >&2
    else
      echo "warning: pr-comment is true but GITHUB_TOKEN/GITHUB_REPOSITORY/a pull_request event isn't available — skipping" >&2
    fi
  fi
fi

exit "$regress_exit"
