#!/usr/bin/env bash
set -euo pipefail

tag="${1:-${PUPBOX_RELEASE_TAG:-}}"
if [[ -z "${tag}" ]]; then
  echo "usage: install-release.sh <release-tag>" >&2
  echo "example: install-release.sh v0.1.0" >&2
  exit 2
fi
if [[ ! "${tag}" =~ ^v[0-9][A-Za-z0-9._-]*$ ]]; then
  echo "invalid release tag: ${tag}" >&2
  echo "expected a tag like v0.1.0" >&2
  exit 2
fi

repo="${PUPBOX_REPO:-https://github.com/donychen1134/pupbox}"
install_root="${PUPBOX_INSTALL_ROOT:-/opt/pupbox}"

machine="$(uname -m)"
case "${machine}" in
  x86_64 | amd64)
    arch="amd64"
    ;;
  aarch64 | arm64)
    arch="arm64"
    ;;
  *)
    echo "unsupported architecture: ${machine}" >&2
    exit 1
    ;;
esac

package="pupbox-linux-${arch}"
url="${repo}/releases/download/${tag}/${package}.tar.gz"
checksums_url="${repo}/releases/download/${tag}/checksums.txt"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

echo "Downloading ${url}"
curl -fL --retry 3 --retry-delay 2 -o "${tmp_dir}/${package}.tar.gz" "${url}"
curl -fL --retry 3 --retry-delay 2 -o "${tmp_dir}/checksums.txt" "${checksums_url}"

expected_checksum="$(awk -v file="${package}.tar.gz" '$2 == file || $2 == "*" file {print $1}' "${tmp_dir}/checksums.txt")"
if [[ ! "${expected_checksum}" =~ ^[a-fA-F0-9]{64}$ ]]; then
  echo "checksum not found for ${package}.tar.gz" >&2
  exit 1
fi
if command -v sha256sum >/dev/null 2>&1; then
  actual_checksum="$(sha256sum "${tmp_dir}/${package}.tar.gz" | awk '{print $1}')"
else
  actual_checksum="$(shasum -a 256 "${tmp_dir}/${package}.tar.gz" | awk '{print $1}')"
fi
if [[ "${actual_checksum}" != "${expected_checksum}" ]]; then
  echo "checksum mismatch for ${package}.tar.gz" >&2
  exit 1
fi
echo "Verified SHA-256 checksum for ${package}.tar.gz"

mkdir -p "${install_root}/releases/${tag}"
tar -xzf "${tmp_dir}/${package}.tar.gz" -C "${tmp_dir}"

rm -rf "${install_root}/releases/${tag:?}/"*
cp -R "${tmp_dir}/${package}/." "${install_root}/releases/${tag}/"
ln -sfn "${install_root}/releases/${tag}" "${install_root}/current"

echo "Installed ${tag} to ${install_root}/current"
