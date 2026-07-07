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

download_optional() {
  url="$1"
  output="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$output" >/dev/null 2>&1 && return 0
    return 1
  fi
  if command -v wget >/dev/null 2>&1; then
    wget -qO "$output" "$url" >/dev/null 2>&1 && return 0
    return 1
  fi
  echo "curl or wget is required" >&2
  exit 1
}

verify_checksum() {
  archive_path="$1"
  checksums_path="$2"
  archive_name="$(basename "$archive_path")"
  expected="$(awk -v name="$archive_name" '$2 == name { print $1; found = 1 } END { if (!found) exit 1 }' "$checksums_path")" || {
    echo "checksum for $archive_name not found" >&2
    exit 1
  }

  if command -v sha256sum >/dev/null 2>&1; then
    printf '%s  %s\n' "$expected" "$archive_path" | sha256sum -c -
    return
  fi
  if command -v shasum >/dev/null 2>&1; then
    actual="$(shasum -a 256 "$archive_path" | awk '{ print $1 }')"
    if [ "$actual" = "$expected" ]; then
      return
    fi
    echo "$archive_name checksum mismatch" >&2
    exit 1
  fi
  echo "sha256sum or shasum is required to verify downloads" >&2
  exit 1
}

download "$base_url/$archive" "$tmp_dir/$archive"
if download_optional "$base_url/checksums.txt" "$tmp_dir/checksums.txt"; then
  verify_checksum "$tmp_dir/$archive" "$tmp_dir/checksums.txt"
else
  echo "checksums.txt not found for this release; installing without checksum verification" >&2
fi
tar -xzf "$tmp_dir/$archive" -C "$tmp_dir"

mkdir -p "$install_dir"
install "$tmp_dir/doit" "$install_dir/doit"

echo "Installed doit to $install_dir/doit"
echo "Run: $install_dir/doit"
