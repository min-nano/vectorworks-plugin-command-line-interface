#!/bin/sh
# cli のフック（scripts/hooks/*.sh）のテスト（docs/plugin/install-and-update.md「テスト」）。
# 外部の枠組みを使わない。HOME と VW2026_HOOK_* には一時ディレクトリを渡し、利用者の場所に触れない。
#
#   sh tests/hooks/hooks_test.sh
set -u

root="$(cd "$(dirname "$0")/../.." && pwd)"
hooks="$root/scripts/hooks"
# shellcheck source=/dev/null
. "$hooks/lib.sh"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
failures=0

fail() {
	printf 'FAIL: %s\n' "$1" >&2
	failures=$((failures + 1))
}

# expect は $2 と $3 が同じでなければ $1 の名前で失敗を数える。
expect() {
	if [ "$2" != "$3" ]; then
		fail "$1: got [$2], want [$3]"
	fi
}

# setup は新しい HOME と、CLI の入った <Plug-Ins>/cli を作る（$1 は <Plug-Ins> の親の名前）。
# home・plugin・link・out・code を置き直す。
setup() {
	case_dir="$work/$1"
	home="$case_dir/home"
	plugin="$case_dir/Plug-Ins/cli"
	link="$home/.local/bin/vw2026"
	mkdir -p "$home" "$plugin/bin"
	: >"$plugin/bin/vw2026"
	out=""
	code=0
}

# run_hook は scripts/hooks/$1.sh をプラグインのフォルダ（$2）で走らせ、標準出力を out、終了コードを
# code に置く。HOME・PATH・VW2026_HOOK_INSTALL_DIR は呼ぶ前に置いた hook_home・hook_path・hook_install_dir。
run_hook() {
	out="$(cd "$2" && HOME="$hook_home" PATH="$hook_path" VW2026_HOOK_INSTALL_DIR="$hook_install_dir" \
		VW2026_HOOK_PLUGIN_DIR="$2" /bin/sh "$hooks/$1.sh" 2>"$work/stderr")"
	code=$?
}

# 既定の PATH（~/.local/bin を含む）で install / uninstall を走らせる。
install_hook() {
	hook_home="$home"
	hook_path="$home/.local/bin:/usr/bin:/bin"
	hook_install_dir="$plugin"
	run_hook install "$plugin"
}
uninstall_hook() {
	hook_home="$home"
	hook_path="/usr/bin:/bin"
	hook_install_dir="$plugin"
	run_hook uninstall "${1:-$plugin}"
}

# --- lib.sh ---

expect "json plain" "$(json_string abc)" '"abc"'
expect "json empty" "$(json_string '')" '""'
expect "json quote and backslash" "$(json_string 'a"b\c')" '"a\"b\\c"'
expect "json japanese" "$(json_string 'プラグイン')" '"プラグイン"'
tab="$(printf '\t')"
expect "json tab" "$(json_string "a${tab}b")" '"a\u0009b"'
newline='
'
expect "json trailing newline" "$(json_string "a${newline}")" '"a\u000a"'
expect "json inner newline" "$(json_string "a${newline}b")" '"a\u000ab"'

expect "strip slashes" "$(strip_slashes /a/b//)" /a/b
expect "strip slashes keeps the root" "$(strip_slashes /)" /

PATH_SAVED="$PATH"
PATH="/usr/bin:/x/y/:/bin"
dir_in_path /x/y || fail "dir_in_path with a trailing slash in PATH"
dir_in_path /x/y/ || fail "dir_in_path with a trailing slash in the argument"
if dir_in_path /x; then
	fail "dir_in_path matched a parent"
fi
# 空の PATH を確かめるため、わざと PATH を空にする（次の行で戻す）。
# shellcheck disable=SC2123
PATH=""
if dir_in_path /x/y; then
	fail "dir_in_path matched an empty PATH"
fi
PATH="$PATH_SAVED"

# same_place は、在る親どうしを同一性で、その下を文字列で比べる。
mkdir -p "$work/same/real/a"
ln -s "$work/same/real" "$work/same/link"
same_place "$work/same/real/a" "$work/same/link/a" || fail "same_place through a link"
same_place "$work/same/real/gone/x" "$work/same/link/gone/x" || fail "same_place below a missing folder"
same_place "$work/same/real/a/" "$work/same/real/a" || fail "same_place with a trailing slash"
if same_place "$work/same/real/gone/x" "$work/same/link/gone/y"; then
	fail "same_place matched another name below a missing folder"
fi
if same_place "$work/same/real/a" "$work/same/real"; then
	fail "same_place matched a parent"
fi
if same_place "same/real/a" "same/real/a"; then
	fail "same_place matched a relative path"
fi

# --- install.sh ---

setup linked
install_hook
expect "install linked: code" "$code" 0
expect "install linked: output" "$out" '{"path":"linked"}'
expect "install linked: link" "$(readlink "$link")" "$plugin/bin/vw2026"
# 2 回目は同じリンクを見つけて何もしない
install_hook
expect "install again: code" "$code" 0
expect "install again: output" "$out" '{"path":"exists"}'
expect "install again: link" "$(readlink "$link")" "$plugin/bin/vw2026"

setup trailing-slash
install_hook
hook_install_dir="$plugin/"
run_hook install "$plugin"
expect "install with a trailing slash: output" "$out" '{"path":"exists"}'

setup not-in-path
hook_home="$home"
hook_path="/usr/bin:/bin"
hook_install_dir="$plugin"
run_hook install "$plugin"
expect "install not_in_path: code" "$code" 0
expect "install not_in_path: output" "$out" '{"path":"not_in_path"}'
expect "install not_in_path: link" "$(readlink "$link")" "$plugin/bin/vw2026"
grep -q 'not in PATH' "$work/stderr" || fail "install not_in_path: no advice on the standard error"
# リンクが既にあっても、PATH に無ければ not_in_path
run_hook install "$plugin"
expect "install not_in_path again: output" "$out" '{"path":"not_in_path"}'

setup user-file
mkdir -p "$home/.local/bin"
printf 'mine\n' >"$link"
install_hook
expect "install over a user file: code" "$code" 0
expect "install over a user file: output" "$out" '{"path":"conflict"}'
expect "install over a user file: file" "$(cat "$link")" mine

setup other-link
mkdir -p "$home/.local/bin"
ln -s "$case_dir/elsewhere/bin/vw2026" "$link"
install_hook
expect "install over another link: output" "$out" '{"path":"conflict"}'
expect "install over another link: link" "$(readlink "$link")" "$case_dir/elsewhere/bin/vw2026"

setup bin-is-a-file
mkdir -p "$home/.local"
printf 'mine\n' >"$home/.local/bin"
install_hook
expect "install when ~/.local/bin is a file: output" "$out" '{"path":"conflict"}'
expect "install when ~/.local/bin is a file: file" "$(cat "$home/.local/bin")" mine

setup no-cli
rm "$plugin/bin/vw2026"
install_hook
expect "install without the CLI: code" "$code" 1
expect "install without the CLI: output" "$out" ""
[ -e "$home/.local" ] && fail "install without the CLI made ~/.local"

setup no-env
hook_home="$home"
hook_path="$home/.local/bin:/usr/bin:/bin"
hook_install_dir=""
run_hook install "$plugin"
expect "install without VW2026_HOOK_INSTALL_DIR: code" "$code" 1
expect "install without VW2026_HOOK_INSTALL_DIR: output" "$out" ""
hook_install_dir="relative/cli"
run_hook install "$plugin"
expect "install with a relative VW2026_HOOK_INSTALL_DIR: code" "$code" 1
[ -e "$home/.local" ] && fail "install without VW2026_HOOK_INSTALL_DIR made ~/.local"

# --- uninstall.sh ---

# uninstall のフックは、フォルダを <Work>/old/cli へ移したあとに、移した先で走る。
setup uninstall-moved
install_hook
moved="$case_dir/work/old/cli"
mkdir -p "$case_dir/work/old"
mv "$plugin" "$moved"
uninstall_hook "$moved"
expect "uninstall: code" "$code" 0
expect "uninstall: output" "$out" '{"path":"removed","left":[]}'
[ -L "$link" ] && fail "uninstall: the link is left"
[ -d "$home/.local/bin" ] || fail "uninstall: removed ~/.local/bin"
# 2 回目は何も無い
uninstall_hook "$moved"
expect "uninstall again: code" "$code" 0
expect "uninstall again: output" "$out" '{"path":"none","left":[]}'

# 同じ <Plug-Ins> を別の書き方（シンボリックリンク）で指して入れたものも、移したあとに消す。install の
# 「同じリンクが既にある」も同じ比べ方。
setup uninstall-other-spelling
ln -s "$case_dir/Plug-Ins" "$case_dir/Plug-Ins-link"
hook_home="$home"
hook_path="$home/.local/bin:/usr/bin:/bin"
hook_install_dir="$case_dir/Plug-Ins-link/cli"
run_hook install "$plugin"
expect "install through a link: output" "$out" '{"path":"linked"}'
install_hook
expect "install again with the real path: output" "$out" '{"path":"exists"}'
moved="$case_dir/work/old/cli"
mkdir -p "$case_dir/work/old"
mv "$plugin" "$moved"
uninstall_hook "$moved"
expect "uninstall with the real path: output" "$out" '{"path":"removed","left":[]}'
[ -L "$link" ] && fail "uninstall with the real path: the link is left"

setup uninstall-other-link
mkdir -p "$home/.local/bin"
ln -s "$case_dir/elsewhere/bin/vw2026" "$link"
uninstall_hook
expect "uninstall with another link: output" "$out" '{"path":"none","left":[]}'
expect "uninstall with another link: link" "$(readlink "$link")" "$case_dir/elsewhere/bin/vw2026"

# 前方一致だけで <Plug-Ins>/cli-other などを指すリンクを消さない
setup uninstall-sibling
mkdir -p "$home/.local/bin"
ln -s "$plugin/bin-other/vw2026" "$link"
uninstall_hook
expect "uninstall with a sibling link: output" "$out" '{"path":"none","left":[]}'
[ -L "$link" ] || fail "uninstall with a sibling link: removed the link"

setup uninstall-user-file
mkdir -p "$home/.local/bin"
printf 'mine\n' >"$link"
uninstall_hook
expect "uninstall with a user file: output" "$out" '{"path":"none","left":[]}'
expect "uninstall with a user file: file" "$(cat "$link")" mine

setup uninstall-no-env
install_hook
hook_install_dir=""
run_hook uninstall "$plugin"
expect "uninstall without VW2026_HOOK_INSTALL_DIR: code" "$code" 1
expect "uninstall without VW2026_HOOK_INSTALL_DIR: output" "$out" ""
[ -L "$link" ] || fail "uninstall without VW2026_HOOK_INSTALL_DIR: removed the link"
hook_install_dir="$plugin"
hook_home=""
run_hook uninstall "$plugin"
expect "uninstall without HOME: code" "$code" 1
[ -L "$link" ] || fail "uninstall without HOME: removed the link"

# 日本語・"・\ を含むパスでも、出力は正しい JSON のままで、リンクを置いて消せる。
setup '日本語 "q" \b'
install_hook
expect "install with a japanese path: output" "$out" '{"path":"linked"}'
expect "install with a japanese path: link" "$(readlink "$link")" "$plugin/bin/vw2026"
uninstall_hook
expect "uninstall with a japanese path: output" "$out" '{"path":"removed","left":[]}'

# 消せないリンクは left に載せ、0 で終わる（root は書けないディレクトリにも書けるので飛ばす）。
if [ "$(id -u)" -ne 0 ]; then
	setup '消せない "q" \b'
	install_hook
	chmod 555 "$home/.local/bin"
	uninstall_hook
	chmod 755 "$home/.local/bin"
	expect "uninstall that cannot remove: code" "$code" 0
	expect "uninstall that cannot remove: output" "$out" \
		"{\"path\":\"failed\",\"left\":[$(json_string "$link")]}"
	case $out in
	*'消せない \"q\" \\b'*) ;;
	*) fail "uninstall that cannot remove: the path is not escaped: $out" ;;
	esac
	[ -L "$link" ] || fail "uninstall that cannot remove: the link is gone"
fi

if [ "$failures" -ne 0 ]; then
	printf '%d failed\n' "$failures" >&2
	exit 1
fi
echo ok
