#!/usr/bin/env bash
set -euo pipefail

show_help() {
	cat <<'EOF'
YAPP uninstaller

Usage:
  bash scripts/uninstall.sh [--help|-h]

This separate script asks before removing ~/.yapp and everything inside it,
including YAPP-installed tools. It then asks whether to remove the exact YAPP
environment source line from Bash and Zsh startup files. A declined prompt
cancels the uninstall.
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

CYAN='' GREEN='' YELLOW='' RED='' RESET=''
if [[ -t 1 ]]; then
	CYAN=$'\033[36m' GREEN=$'\033[32m' YELLOW=$'\033[33m' RED=$'\033[31m' RESET=$'\033[0m'
fi

step() { printf '\n%b==>%b %s\n' "$CYAN" "$RESET" "$1"; }
success() { printf '%b✓%b %s\n' "$GREEN" "$RESET" "$1"; }
warn() { printf '%b!%b %s\n' "$YELLOW" "$RESET" "$1" >&2; }
fail() { printf '%bError:%b %s\n' "$RED" "$RESET" "$1" >&2; }

confirm() {
	local answer
	printf '%s [y/N] ' "$1" >&2
	[[ -t 0 || -t 1 || -t 2 ]] || return 1
	IFS= read -r answer 2>/dev/null </dev/tty || return 1
	case "$answer" in
		[yY]|[yY][eE][sS]) return 0 ;;
		*) return 1 ;;
	esac
}

USER_HOME=${HOME:?HOME must be set}
if [[ $USER_HOME != /* || $USER_HOME == / ]]; then
	fail "Refusing to uninstall with an unsafe HOME value."
	exit 1
fi
YAPP_HOME=$USER_HOME/.yapp
if [[ -L $YAPP_HOME || ( -e $YAPP_HOME && ! -d $YAPP_HOME ) ]]; then
	fail "Refusing to remove $YAPP_HOME because it is not a real directory."
	exit 1
fi

SOURCE_LINE='. "${HOME}/.yapp/yapp-env.sh"'
declare -a STARTUP_FILES=()
shell_name=$(basename "${SHELL:-}")
case "$shell_name:$(uname -s)" in
	bash:Darwin) name=.bash_profile ;;
	bash:*) name=.bashrc ;;
	zsh:Darwin) name=.zprofile ;;
	zsh:*) name=.zshrc ;;
	*) name= ;;
esac
if [[ -n $name ]]; then
	rc=$USER_HOME/$name
	if [[ -L $rc ]]; then
		if grep -Fqx "$SOURCE_LINE" "$rc" 2>/dev/null; then
			fail "Refusing to edit $rc because it is a symbolic link."
			exit 1
		fi
	elif [[ -e $rc && ! -f $rc ]]; then
		fail "Refusing to edit $rc because it is not a regular file."
		exit 1
	elif [[ -f $rc ]] && grep -Fqx "$SOURCE_LINE" "$rc"; then
		STARTUP_FILES+=("$rc")
	fi
fi

if [[ ! -e $YAPP_HOME && ${#STARTUP_FILES[@]} -eq 0 ]]; then
	success "YAPP installation and startup entry were not found."
	exit 0
fi

step "Reviewing YAPP removal"
if [[ -d $YAPP_HOME ]]; then
	printf 'This will remove %s and everything inside it, including YAPP and all tools installed by YAPP.\n' "$YAPP_HOME"
	printf 'Close any running YAPP commands before continuing.\n'
else
	printf 'YAPP files were not found; only shell startup entries may be removed.\n'
fi
if ! confirm "Continue with YAPP removal?"; then
	success "Cancelled; no files changed."
	exit 0
fi

if (( ${#STARTUP_FILES[@]} > 0 )); then
	step "Remove the YAPP shell startup line?"
	printf 'The exact line . "${HOME}/.yapp/yapp-env.sh" appears in:\n'
	printf '  %s\n' "${STARTUP_FILES[@]}"
	if ! confirm "Remove this line from these files?"; then
		success "Cancelled; no files changed."
		exit 0
	fi
fi

for rc in "${STARTUP_FILES[@]}"; do
	step "Removing YAPP startup entry from $rc"
	tmp=$(mktemp "${rc}.yapp.XXXXXXXX")
	if ! awk -v source="$SOURCE_LINE" '$0 != source { print }' "$rc" > "$tmp"; then
		rm -f -- "$tmp"
		fail "Could not update $rc."
		exit 1
	fi
	if ! cat "$tmp" > "$rc"; then
		rm -f -- "$tmp"
		fail "Could not update $rc."
		exit 1
	fi
	rm -f -- "$tmp"
done

if [[ -d $YAPP_HOME ]]; then
	step "Removing $YAPP_HOME"
	rm -rf -- "$YAPP_HOME"
fi

success "YAPP uninstalled. Open a new shell to load the updated startup configuration."
