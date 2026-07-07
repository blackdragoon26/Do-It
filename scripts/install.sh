#!/usr/bin/env sh
set -eu

repo="blackdragoon26/Do-It"
install_dir="${DOIT_INSTALL_DIR:-$HOME/.local/bin}"
version="${DOIT_VERSION:-latest}"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"

case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) echo "unsupported architecture: $arch" >&2; exit 1 ;;
esac

case "$os" in
  linux|darwin) archive_ext="tar.gz" ;;
  *) echo "unsupported OS: $os" >&2; exit 1 ;;
esac

if [ "$version" = "latest" ]; then
  base_url="https://github.com/$repo/releases/latest/download"
else
  base_url="https://github.com/$repo/releases/download/$version"
fi

archive="Do-It_${os}_${arch}.${archive_ext}"
tmp_dir="$(mktemp -d 2>/dev/null || mktemp -d "${TMPDIR:-/tmp}/doit.XXXXXX")"
trap 'rm -rf "$tmp_dir"' EXIT INT TERM

download() {
  url="$1"
  output="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$output"
    return
  fi
  if command -v wget >/dev/null 2>&1; then
    wget -qO "$output" "$url"
    return
  fi
  echo "curl or wget is required" >&2
  exit 1
}

download "$base_url/$archive" "$tmp_dir/$archive"
tar -xzf "$tmp_dir/$archive" -C "$tmp_dir"

mkdir -p "$install_dir"
install "$tmp_dir/doit" "$install_dir/doit"

echo "Installed doit to $install_dir/doit"
echo "Run: $install_dir/doit"
