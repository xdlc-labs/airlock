#!/usr/bin/env bash
# Snapshot merge-base vs HEAD and run airlock ci. Used by action.yml.
set -euo pipefail

AIRLOCK="${AIRLOCK:-/tmp/airlock}"
FAIL_ON_APPROVAL="${FAIL_ON_APPROVAL:-true}"
FAIL_ON_EVAL="${FAIL_ON_EVAL:-false}"
FAIL_ON_AI_CHANGE="${FAIL_ON_AI_CHANGE:-false}"
FAIL_ON_INCONCLUSIVE="${FAIL_ON_INCONCLUSIVE:-false}"

if [ "${GITHUB_EVENT_NAME:-}" = "pull_request" ]; then
  BASE_REF="origin/${GITHUB_BASE_REF}"
  git fetch origin "${GITHUB_BASE_REF}" 2>/dev/null || true
  BASE_SHA=$(git merge-base "$BASE_REF" HEAD 2>/dev/null || git rev-parse HEAD~1 2>/dev/null || git rev-parse HEAD)
  HEAD_SHA=${GITHUB_SHA}
else
  HEAD_SHA=$(git rev-parse HEAD)
  BASE_SHA=$(git rev-parse HEAD~1 2>/dev/null || echo "$HEAD_SHA")
fi

work=$(mktemp -d)
git worktree add --detach "$work/base" "$BASE_SHA"
git worktree add --detach "$work/head" "$HEAD_SHA"

(cd "$work/base" && "$AIRLOCK" init && "$AIRLOCK" snapshot)
BASE_ID=$(basename "$(ls -1t "$work/base/.airlock/snapshots/"*.json | head -1)" .json)

mkdir -p "$work/head/.airlock/snapshots"
cp -a "$work/base/.airlock/snapshots/." "$work/head/.airlock/snapshots/"
(cd "$work/head" && "$AIRLOCK" init && "$AIRLOCK" snapshot)

CI_FLAGS=(--base "$BASE_ID" --head working --comment)
if [ "$FAIL_ON_APPROVAL" = "true" ]; then
  CI_FLAGS+=(--fail-on-approval)
fi
if [ "$FAIL_ON_EVAL" = "true" ]; then
  CI_FLAGS+=(--fail-on-eval)
fi
if [ "$FAIL_ON_INCONCLUSIVE" = "true" ]; then
  CI_FLAGS+=(--fail-on-inconclusive)
fi
(cd "$work/head" && "$AIRLOCK" ci "${CI_FLAGS[@]}") | tee /tmp/airlock-comment.md

if [ "$FAIL_ON_AI_CHANGE" = "true" ]; then
  (cd "$work/head" && "$AIRLOCK" ci --base "$BASE_ID" --head working --fail-on-change)
fi

git worktree remove --force "$work/base" || true
git worktree remove --force "$work/head" || true
