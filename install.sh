#!/usr/bin/env bash
# Install airlock into the first writable of: $BINDIR, $GOBIN, $GOPATH/bin, $HOME/bin, /usr/local/bin
set -euo pipefail

REPO="xdlc-labs/airlock"
BIN_NAME="airlock"

bin_dir() {
  if [[ -n "${BINDIR:-}" ]]; then echo "$BINDIR"; return; fi
  if [[ -n "${GOBIN:-}" ]]; then echo "$GOBIN"; return; fi
  if [[ -n "${GOPATH:-}" ]]; then echo "${GOPATH%/}/bin"; return; fi
  if [[ -d "$HOME/bin" || -w "$HOME" ]]; then echo "$HOME/bin"; return; fi
  echo "/usr/local/bin"
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || return 1
}

install_from_release() {
  need_cmd curl || return 1
  need_cmd tar || return 1
  local os arch tag url tmp dest="$1"
  os=$(uname -s | tr '[:upper:]' '[:lower:]')
  arch=$(uname -m)
  case "$arch" in
    x86_64 | amd64) arch=amd64 ;;
    aarch64 | arm64) arch=arm64 ;;
    *) echo "error: unsupported arch: $arch" >&2; return 1 ;;
  esac
  local ext=tar.gz bin="$BIN_NAME"
  case "$os" in
    linux | darwin) ;;
    mingw* | msys* | cygwin*)
      # Git Bash / MSYS2 on Windows: releases carry a zip with airlock.exe.
      os=windows; ext=zip; bin="${BIN_NAME}.exe"
      need_cmd unzip || return 1
      ;;
    *) echo "error: unsupported OS: $os" >&2; return 1 ;;
  esac

  tag="${AIRLOCK_VERSION:-}"
  if [[ -z "$tag" ]]; then
    # Newest stable release. Pre-releases are skipped here: pin AIRLOCK_VERSION
    # to install one of those.
    tag=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1) || true
  fi
  if [[ -z "$tag" ]]; then
    return 1
  fi
  # allow AIRLOCK_VERSION=1.0.0 or v1.0.0
  case "$tag" in
    v*) ;;
    *) tag="v${tag}" ;;
  esac
  url="https://github.com/${REPO}/releases/download/${tag}/${BIN_NAME}_${tag#v}_${os}_${arch}.${ext}"
  tmp=$(mktemp -d)
  if ! curl -fsSL "$url" -o "$tmp/airlock.$ext"; then
    echo "error: no asset at $url" >&2
    rm -rf "$tmp"
    return 1
  fi
  if [[ "$ext" == zip ]]; then
    unzip -q "$tmp/airlock.$ext" -d "$tmp"
  else
    tar -xzf "$tmp/airlock.$ext" -C "$tmp"
  fi
  install -m 755 "$tmp/$bin" "$dest/$bin"
  rm -rf "$tmp"
  echo "installed $dest/$bin ($tag)"
}

install_from_source_tree() {
  need_cmd go || return 1
  local dest="$1"
  local root
  # curl|sh: $0 is often "sh" / "-" — no local tree
  root=$(cd "$(dirname "$0")" 2>/dev/null && pwd) || return 1
  if [[ ! -f "$root/cmd/airlock/main.go" ]]; then
    return 1
  fi
  mkdir -p "$dest"
  (cd "$root" && go build -o "$dest/$BIN_NAME" ./cmd/airlock)
  echo "installed $dest/$BIN_NAME (local source)"
}

install_with_go() {
  need_cmd go || return 1
  local dest="$1"
  local tag="${AIRLOCK_VERSION:-latest}"
  case "$tag" in
    latest | v*) ;;
    *) tag="v${tag}" ;;
  esac
  mkdir -p "$dest"
  GOBIN="$dest" go install "github.com/${REPO}/cmd/airlock@${tag}"
  echo "installed $dest/$BIN_NAME (go install @${tag})"
}

main() {
  local dest
  dest=$(bin_dir)
  mkdir -p "$dest"
  # try release first; keep stderr so download failures are visible
  if install_from_release "$dest"; then
    :
  elif install_from_source_tree "$dest"; then
    :
  else
    echo "no release binary; trying go install…" >&2
    if ! install_with_go "$dest"; then
      echo "error: install failed. Try:" >&2
      echo "  go install github.com/${REPO}/cmd/airlock@latest" >&2
      echo "Or clone and: go build -o airlock ./cmd/airlock" >&2
      exit 1
    fi
  fi
  if ! command -v "$BIN_NAME" >/dev/null 2>&1; then
    echo "add to PATH: export PATH=\"$dest:\$PATH\"" >&2
  fi
  "$dest/$BIN_NAME" version || true
}

main "$@"
