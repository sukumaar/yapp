#!/usr/bin/env bash
set -euo pipefail

show_help() {
	cat <<'EOF'
YAPP source installer

Usage:
  scripts/install.sh [--help|-h]

Builds YAPP from this checkout and installs it under ~/.yapp/lib/yapp, with
the yapp command linked from ~/.yapp/bin/yapp. Other files in ~/.yapp stay
as they are. The script asks before replacing an existing yapp binary or
command link. It adds one environment-file source block to Bash or Zsh startup
files; open a new shell or source the environment file to use yapp.

The script uses a Go version from PATH if it meets the project's requirement.
Otherwise, it asks to download a temporary Go toolchain and verifies its
checksum before building YAPP. Download and build progress appears in the
terminal.

Requirements:
  bash and Go, or curl, tar, and sha256sum/shasum to download Go
  Go downloads support Linux and macOS on amd64 and arm64.

Run the installed program with:
  yapp
EOF
}

case "${1:-}" in
	-h|--help)
		if (( $# > 1 )); then
			printf 'Usage: %s [--help|-h]\n' "$0" >&2
			exit 2
		fi
		show_help
		exit 0
		;;
	'') ;;
	*)
		printf 'Unknown option: %s\nUsage: %s [--help|-h]\n' "$1" "$0" >&2
		exit 2
		;;
esac

SHELL_NAME=${SHELL:-}
SHELL_NAME=${SHELL_NAME##*/}
case "$SHELL_NAME" in
	bash|zsh) ;;
	*)
		printf 'Set SHELL to Bash or Zsh to configure the yapp command PATH.\n' >&2
		exit 1
		;;
esac

GO_BOOTSTRAP_VERSION=1.27.1
ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
TARGET_DIR=${HOME:?HOME must be set}/.yapp
LIB_DIR=$TARGET_DIR/lib
BIN_DIR=$TARGET_DIR/bin
TARGET=$LIB_DIR/yapp
COMMAND=$BIN_DIR/yapp
REPLACE_CONFIRMED=0
CYAN='' GREEN='' YELLOW='' RED='' RESET=''
if [[ -t 1 ]]; then
	CYAN=$'\033[36m' GREEN=$'\033[32m' YELLOW=$'\033[33m' RED=$'\033[31m' RESET=$'\033[0m'
fi

step() { printf '\n%b==>%b %s\n' "$CYAN" "$RESET" "$1"; }
success() { printf '%b✓%b %s\n' "$GREEN" "$RESET" "$1"; }
warn() { printf '%b!%b %s\n' "$YELLOW" "$RESET" "$1" >&2; }
fail() { printf '%bError:%b %s\n' "$RED" "$RESET" "$1" >&2; }

configure_path() {
	local shell_name=$SHELL_NAME rc os_name
	os_name=$(uname -s)
	local begin='# >>> yapp managed >>>' end='# <<< yapp managed <<<'
	local source='. "${HOME}/.yapp/yapp-env.sh"'
	local tmp
	local -a rc_files
	case "$shell_name" in
		bash)
			if [[ $os_name == Darwin ]]; then rc_files=("$HOME/.bash_profile"); else rc_files=("$HOME/.bashrc"); fi
			;;
		zsh)
			if [[ $os_name == Darwin ]]; then rc_files=("$HOME/.zprofile"); else rc_files=("$HOME/.zshrc"); fi
			;;
		*) fail "Set SHELL to Bash or Zsh to configure the yapp command PATH."; return 1 ;;
	esac
	for rc in "${rc_files[@]}"; do
		if [[ -L $rc || ( -e $rc && ! -f $rc ) ]]; then
			fail "Refusing to edit $rc because it is not a regular file."
			return 1
		fi
		if [[ -f $rc ]] && { grep -Fqx "$begin" "$rc" || grep -Fqx "$end" "$rc"; }; then
			if ! grep -Fqx "$begin" "$rc" || ! grep -Fqx "$end" "$rc"; then
				fail "$rc contains an incomplete YAPP managed block."
				return 1
			fi
			tmp=$(mktemp "${rc}.yapp.XXXXXXXX")
			awk -v start="$begin" -v finish="$end" '
				$0 == start { print ". \"${HOME}/.yapp/yapp-env.sh\""; skip=1; next }
				$0 == finish { skip=0; next }
				!skip { print }
			' "$rc" > "$tmp"
			cat "$tmp" > "$rc"
			rm -f -- "$tmp"
			continue
		fi
		if [[ -f $rc ]] && grep -Fq "$source" "$rc"; then
			continue
		fi
		printf '\n%s\n' "$source" >> "$rc"
		success "Added YAPP environment loading to $rc"
	done
}

initialize_environment() {
	local env_file=$TARGET_DIR/yapp-env.sh tmp
	[[ -e $env_file || -L $env_file ]] && return 0
	tmp=$(mktemp "$TARGET_DIR/.yapp-env.XXXXXXXX")
	cat > "$tmp" <<'EOF'
export YAPP_HOME="${HOME}/.yapp"
case ":${PATH:-}:" in
  *":${YAPP_HOME}/bin:"*) ;;
  *) export PATH="${YAPP_HOME}/bin${PATH:+:${PATH}}" ;;
esac
EOF
	chmod 600 "$tmp"
	mv -- "$tmp" "$env_file"
}

confirm() {
	local answer
	printf '%s [y/N] ' "$1" >&2
	[[ -t 0 || -t 1 || -t 2 ]] || return 1
	IFS= read -r answer 2>/dev/null </dev/tty || return 1
	case "${answer,,}" in
		y|yes) return 0 ;;
		*) return 1 ;;
	esac
}

required_version=$(awk '$1 == "go" { print $2; exit }' "$ROOT/go.mod")
if [[ ! $required_version =~ ^([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
	fail "Cannot read the required Go version from go.mod."
	exit 1
fi
required_major=${BASH_REMATCH[1]}
required_minor=${BASH_REMATCH[2]}
required_patch=${BASH_REMATCH[3]}

version_satisfies() {
	local version=$1 major minor patch
	if [[ ! $version =~ ^(go)?([0-9]+)\.([0-9]+)\.([0-9]+) ]]; then
		return 1
	fi
	major=${BASH_REMATCH[2]}
	minor=${BASH_REMATCH[3]}
	patch=${BASH_REMATCH[4]}
	(( major > required_major ||
		(major == required_major && minor > required_minor) ||
		(major == required_major && minor == required_minor && patch >= required_patch) ))
}

if ! version_satisfies "$GO_BOOTSTRAP_VERSION"; then
	fail "Bootstrap Go $GO_BOOTSTRAP_VERSION is older than the version required by go.mod ($required_version)."
	exit 1
fi

if [[ -L $TARGET_DIR || ( -e $TARGET_DIR && ! -d $TARGET_DIR ) ]]; then
	fail "Refusing to use $TARGET_DIR because it is not a real directory."
	exit 1
fi
if [[ -e $TARGET && -d $TARGET && ! -L $TARGET ]]; then
	fail "Refusing to replace $TARGET because it is a directory."
	exit 1
fi
if [[ -e $COMMAND || -L $COMMAND ]]; then
	if [[ ! -L $COMMAND || $(readlink -- "$COMMAND") != ../lib/yapp ]]; then
		fail "Refusing to replace $COMMAND because it is not YAPP's command link."
		exit 1
	fi
fi
if [[ -e $TARGET || -L $TARGET || -L $COMMAND ]]; then
	if ! confirm "An existing YAPP binary or command link is present. Replace YAPP's files?"; then
		success "Cancelled; no files changed."
		exit 0
	fi
	REPLACE_CONFIRMED=1
fi

GO=$(command -v go || true)
if [[ -n $GO ]]; then
	GO_VERSION=$("$GO" version 2>/dev/null | awk '{ print $3 }' || true)
fi
if [[ -z ${GO_VERSION:-} ]] || ! version_satisfies "$GO_VERSION"; then
	if [[ -n ${GO_VERSION:-} ]]; then
		warn "Go ${GO_VERSION#go} is too old; this project requires Go $required_version or newer."
	else
		warn "Go was not found in PATH; this project requires Go $required_version or newer."
	fi
	if ! confirm "Download Go $GO_BOOTSTRAP_VERSION temporarily to build YAPP?"; then
		success "Cancelled; no files changed."
		exit 0
	fi

	case "$(uname -s):$(uname -m)" in
		Linux:x86_64)
			GO_OS=linux GO_ARCH=amd64
			GO_SHA256=63d339f0da5ab53635a56f2490a7984dfe12dfcff22ad749f63edaf590168445
			;;
		Linux:aarch64|Linux:arm64)
			GO_OS=linux GO_ARCH=arm64
			GO_SHA256=3450b45a3f9ee8568792736a5c5e70a1f2e9b36c35a8f74958c03e51d7d92bec
			;;
		Darwin:x86_64)
			GO_OS=darwin GO_ARCH=amd64
			GO_SHA256=8f8f52c6649542cf027bbc9b9c68d1ec042f9f34808a40413f0b8b3f66f3caa4
			;;
		Darwin:arm64)
			GO_OS=darwin GO_ARCH=arm64
			GO_SHA256=ee215d57e0ec269c60cc9ceca68e6bda321ba9ee5afe24f4b0988703c2d87d12
			;;
		*)
			fail "Temporary Go download is not configured for $(uname -s)/$(uname -m)."
			exit 1
			;;
	esac
	if ! command -v curl >/dev/null || ! command -v tar >/dev/null; then
		fail "curl and tar are required to download Go."
		exit 1
	fi
	if ! command -v sha256sum >/dev/null && ! command -v shasum >/dev/null; then
		fail "sha256sum or shasum is required to verify the Go download."
		exit 1
	fi
	WORK_DIR=$(mktemp -d)
	ARCHIVE="$WORK_DIR/go.tar.gz"
	URL="https://go.dev/dl/go${GO_BOOTSTRAP_VERSION}.${GO_OS}-${GO_ARCH}.tar.gz"
	step "Downloading Go $GO_BOOTSTRAP_VERSION"
	if [[ -t 2 ]]; then
		curl -fL --retry 2 --progress-bar --output "$ARCHIVE" "$URL"
	else
		curl -fL --retry 2 --output "$ARCHIVE" "$URL"
	fi
	step "Verifying Go download"
	if command -v sha256sum >/dev/null; then
		ACTUAL_SHA256=$(sha256sum "$ARCHIVE" | awk '{ print $1 }')
	else
		ACTUAL_SHA256=$(shasum -a 256 "$ARCHIVE" | awk '{ print $1 }')
	fi
	if [[ $ACTUAL_SHA256 != "$GO_SHA256" ]]; then
		fail "Go download checksum mismatch; refusing to use it."
		exit 1
	fi
	success "Go archive checksum verified"
	step "Preparing temporary Go toolchain"
	tar -C "$WORK_DIR" -xzf "$ARCHIVE"
	GO="$WORK_DIR/go/bin/go"
	success "Go $GO_BOOTSTRAP_VERSION ready"
else
	success "Using $GO_VERSION from PATH"
	WORK_DIR=$(mktemp -d)
fi
trap 'rm -rf -- "$WORK_DIR"; if [[ -n ${INSTALL_TMP:-} ]]; then rm -f -- "$INSTALL_TMP"; fi' EXIT

step "Building YAPP with $("$GO" version)"
(cd "$ROOT" && GOTOOLCHAIN=local "$GO" build -v -trimpath -o "$WORK_DIR/yapp" ./cmd/yapp)
success "Build complete"

if [[ ! -e $TARGET_DIR ]]; then
	step "Creating $TARGET_DIR"
	mkdir -m 700 -- "$TARGET_DIR"
fi
if [[ -L $TARGET_DIR || ! -d $TARGET_DIR ]]; then
	fail "Refusing to install because $TARGET_DIR is not a real directory."
	exit 1
fi
for directory in "$LIB_DIR" "$BIN_DIR"; do
	if [[ -L $directory || ( -e $directory && ! -d $directory ) ]]; then
		fail "Refusing to use $directory because it is not a real directory."
		exit 1
	fi
	if [[ ! -e $directory ]]; then
		step "Creating $directory"
		mkdir -m 700 -- "$directory"
	fi
done
INSTALL_TMP=$(mktemp "$TARGET_DIR/.yapp-install.XXXXXXXX")
step "Installing YAPP to $TARGET"
cp -- "$WORK_DIR/yapp" "$INSTALL_TMP"
chmod 755 "$INSTALL_TMP"

if [[ -e $TARGET || -L $TARGET ]]; then
	if (( ! REPLACE_CONFIRMED )); then
		fail "$TARGET appeared during the build; refusing to replace it without confirmation."
		exit 1
	fi
fi
mv -f -- "$INSTALL_TMP" "$TARGET"
INSTALL_TMP=
if [[ -e $COMMAND || -L $COMMAND ]]; then
	if (( ! REPLACE_CONFIRMED )); then
		fail "$COMMAND appeared during the build; refusing to replace it without confirmation."
		exit 1
	fi
	rm -f -- "$COMMAND"
fi
ln -s ../lib/yapp "$COMMAND"
success "Installed YAPP at $TARGET and linked $COMMAND"
step "Preparing the YAPP shell environment"
initialize_environment
step "Configuring the YAPP shell startup"
configure_path
success "Open a new shell, then run: yapp"
printf 'Or in this shell, run:\n  . "${HOME}/.yapp/yapp-env.sh"\n  yapp\n'
