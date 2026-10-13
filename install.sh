#!/usr/bin/env bash
# Installs the fast-resume-scoring binaries (score, extract, check, resume-scoring-mcp) from
# a GitHub release and the default tuning file.
#
#   curl -fsSL https://raw.githubusercontent.com/dphbfs/fast-resume-scoring/main/install.sh | bash
#   curl -fsSL .../install.sh | bash -s -- --installation-dir /usr/local/bin --version v0.1.0
#
# Binaries go to ~/.local/bin unless --installation-dir says otherwise. The
# tuning file goes to ${XDG_CONFIG_HOME:-~/.config}/fast-resume-scoring/:
# tuning.yaml is yours to edit and is never overwritten; tuning.default.yaml
# is the copy shipped with the installed version. Linux and macOS only; on
# Windows, download the zip from the releases page.
set -euo pipefail

REPO="dphbfs/fast-resume-scoring"
BINARIES=(score extract check resume-scoring-mcp)

install_dir="${HOME}/.local/bin"
version="latest"
config_dir="${XDG_CONFIG_HOME:-${HOME}/.config}/fast-resume-scoring"

usage() {
	cat <<EOF
usage: install.sh [--installation-dir DIR] [--version vX.Y.Z]

  --installation-dir DIR  where to put the binaries (default: ~/.local/bin)
  --version vX.Y.Z        release to install (default: latest)

The tuning file goes to ${config_dir}.
EOF
}

die() {
	echo "install.sh: $*" >&2
	exit 1
}

while [ $# -gt 0 ]; do
	case "$1" in
	--installation-dir)
		[ $# -ge 2 ] || die "--installation-dir needs a value"
		install_dir="$2"
		shift 2
		;;
	--installation-dir=*)
		install_dir="${1#*=}"
		shift
		;;
	--version)
		[ $# -ge 2 ] || die "--version needs a value"
		version="$2"
		shift 2
		;;
	--version=*)
		version="${1#*=}"
		shift
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		usage >&2
		die "unknown argument: $1"
		;;
	esac
done

if command -v curl >/dev/null 2>&1; then
	fetch() { curl -fsSL --proto '=https' --tlsv1.2 -o "$2" "$1"; }
	latest_tag() { curl -fsSLI --proto '=https' --tlsv1.2 -o /dev/null -w '%{url_effective}' "https://github.com/${REPO}/releases/latest"; }
elif command -v wget >/dev/null 2>&1; then
	fetch() { wget -q --https-only -O "$2" "$1"; }
	latest_tag() { wget -q --https-only --max-redirect=5 -S --spider "https://github.com/${REPO}/releases/latest" 2>&1 | sed -n 's/^ *[Ll]ocation: *//p' | tail -n1 | tr -d '\r'; }
else
	die "need curl or wget"
fi

case "$(uname -s)" in
Linux) os=linux ;;
Darwin) os=darwin ;;
*) die "unsupported OS $(uname -s); download a release from https://github.com/${REPO}/releases" ;;
esac
case "$(uname -m)" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) die "unsupported architecture $(uname -m)" ;;
esac

if [ "$version" = "latest" ]; then
	version="$(latest_tag)"
	version="${version##*/}"
	[ -n "$version" ] && [ "$version" != "latest" ] || die "could not find the latest release"
fi
case "$version" in
v*) ;;
*) version="v${version}" ;;
esac

archive="fast-resume-scoring_${version#v}_${os}_${arch}.tar.gz"
base="https://github.com/${REPO}/releases/download/${version}"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "Downloading fast-resume-scoring ${version} (${os}/${arch})"
fetch "${base}/${archive}" "${tmp}/${archive}" || die "download failed: ${base}/${archive}"
fetch "${base}/checksums.txt" "${tmp}/checksums.txt" || die "download failed: ${base}/checksums.txt"

want="$(awk -v f="$archive" '$2 == f { print $1 }' "${tmp}/checksums.txt")"
[ -n "$want" ] || die "${archive} is not in checksums.txt"
if command -v sha256sum >/dev/null 2>&1; then
	got="$(sha256sum "${tmp}/${archive}" | awk '{ print $1 }')"
else
	got="$(shasum -a 256 "${tmp}/${archive}" | awk '{ print $1 }')"
fi
[ "$got" = "$want" ] || die "checksum mismatch for ${archive}"

# cosign_flags prints the verify-blob flags this cosign needs to read the
# release's Sigstore bundle (signed by cosign v3), or fails when it cannot:
# v2.5+ reads it as is, v2.4.1+ needs --new-bundle-format, older cannot.
# An unrecognized version is tried as is.
cosign_flags() {
	local v major minor patch
	v="$(cosign version 2>/dev/null | sed -n 's/^GitVersion: *v\{0,1\}//p')"
	IFS=. read -r major minor patch <<<"${v%%-*}"
	case "${major}.${minor}.${patch}" in
	[0-9]*.[0-9]*.[0-9]*) ;;
	*) return 0 ;;
	esac
	if ((major > 2 || (major == 2 && minor >= 5))); then
		return 0
	elif ((major == 2 && minor == 4 && patch >= 1)); then
		echo --new-bundle-format
	else
		return 1
	fi
}

# checksums.txt is signed keylessly in CI; check it when cosign is around.
if command -v cosign >/dev/null 2>&1; then
	if flags="$(cosign_flags)"; then
		fetch "${base}/checksums.txt.sigstore.json" "${tmp}/checksums.txt.sigstore.json" || die "download failed: checksums.txt.sigstore.json"
		# shellcheck disable=SC2086 # flags is empty or one word
		cosign verify-blob ${flags} --bundle "${tmp}/checksums.txt.sigstore.json" \
			--certificate-identity-regexp "^https://github.com/${REPO}/" \
			--certificate-oidc-issuer https://token.actions.githubusercontent.com \
			"${tmp}/checksums.txt" >/dev/null 2>&1 || die "cosign could not verify checksums.txt"
		echo "Verified the release signature with cosign"
	else
		echo "Skipped the release signature check: it needs cosign v2.4.1 or newer (the checksum was verified)" >&2
	fi
fi

mkdir -p "${tmp}/x"
tar -xzf "${tmp}/${archive}" -C "${tmp}/x"

mkdir -p "$install_dir"
for b in "${BINARIES[@]}"; do
	[ -f "${tmp}/x/${b}" ] || die "${b} is missing from ${archive}"
	install -m 0755 "${tmp}/x/${b}" "${install_dir}/${b}"
done
echo "Installed ${BINARIES[*]} to ${install_dir}"

mkdir -p "$config_dir"
install -m 0644 "${tmp}/x/tuning.yaml" "${config_dir}/tuning.default.yaml"
if [ -e "${config_dir}/tuning.yaml" ]; then
	echo "Kept your ${config_dir}/tuning.yaml; this version's defaults are in tuning.default.yaml"
else
	install -m 0644 "${tmp}/x/tuning.yaml" "${config_dir}/tuning.yaml"
	echo "Wrote the tuning file to ${config_dir}/tuning.yaml"
fi
[ -e "${config_dir}/.env.example" ] || install -m 0644 "${tmp}/x/.env.example" "${config_dir}/.env.example"

case ":${PATH}:" in
*":${install_dir}:"*) ;;
*) printf '\n%s is not on your PATH. Add it:\n  export PATH="%s:$PATH"\n' "$install_dir" "$install_dir" ;;
esac

cat <<EOF

Next:
  export TYPESAFE_API_KEY=...                           # Jev key from TypeSafe or OpenRouter
  export TUNING_FILE="${config_dir}/tuning.yaml"   # only if you edit the tuning file
  score -jd job.md -resume resume.md
EOF
