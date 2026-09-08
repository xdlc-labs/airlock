#!/usr/bin/env bash
# Regenerates the README demo. Run from a clone of this repository:
#
#   asciinema rec --cols 84 --rows 34 -q -i 1.5 -c docs/assets/demo.sh docs/assets/demo.cast
#   agg --font-size 15 --theme asciinema --fps-cap 20 docs/assets/demo.cast docs/assets/demo.gif
#
# It copies the toy agent to a scratch directory so the repository is not
# touched, and uses only the replay cassettes, so no API keys are involved.
set -u

repo=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

go build -o "$work/airlock" "$repo/cmd/airlock" || exit 1
export PATH="$work:$PATH"

# Only the files git tracks, so the demo shows a fresh-clone experience.
(cd "$repo" && git ls-files testdata/toy-agent && echo testdata/toy-agent/.airlock/policy.yml) |
  while read -r f; do
    mkdir -p "$work/agent/$(dirname "${f#testdata/toy-agent/}")"
    cp "$repo/$f" "$work/agent/${f#testdata/toy-agent/}"
  done
cd "$work/agent" || exit 1

prompt=$'\033[1;36m$\033[0m '
say() { printf "\033[2m%s\033[0m\n" "$1"; sleep 1.1; }
type_out() {
  printf '%s' "$prompt"
  local i
  for ((i = 0; i < ${#1}; i++)); do
    printf '%s' "${1:$i:1}"
    sleep 0.018
  done
  printf '\n'
  sleep 0.35
}
run() {
  type_out "$1"
  eval "$1" 2>&1 | grep -v '^artifact ->\|^MCP change detected'
  sleep 1.0
}

say "# freeze what the AI system is right now"
run "airlock init && airlock snapshot"

say "# record a green baseline (replay cassettes, no API keys)"
run "airlock test --mode replay"

say "# now a coding agent opens a PR. one line in apm.lock.yaml:"
sleep 0.3
printf "\033[2m#\033[0m   mcp: local-fs: permissions: [read\033[1;31m, write\033[0m]\n"
sed -i 's/^      - read$/      - read\n      - write/' apm.lock.yaml
sleep 1.4

say "# evals still pass. does it ship?"
run "airlock ci --fail-on-approval"
printf "\033[1;31mexit 1\033[0m  \033[2m- blocked until a human approves\033[0m\n"
sleep 2.6
