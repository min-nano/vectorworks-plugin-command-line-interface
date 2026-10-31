#!/bin/sh
# cli の uninstall のフック（mac）。~/.local/bin/vw2026 が <Plug-Ins>/cli/bin/vw2026 を指すリンク（同じ
# ディレクトリを別の書き方で指すものも含む。lib.sh の link_points_to）のときだけ消し、標準出力に
# {"path":…,"left":[…]} を 1 つだけ書く
# （docs/plugin/install-and-update.md「`uninstall` のフック」）。
#
#   removed  リンクを消した
#   none     このプラグインの CLI を指すリンクが無かった（利用者のファイル・別の場所を指すリンクには触れない）
#   failed   リンクを消せなかった（left に載せる）
#
# フォルダは <Work>/old/cli/ へ移したあとで走る（VW2026_HOOK_PLUGIN_DIR は移した先）ので、消すものは
# VW2026_HOOK_INSTALL_DIR から決める。消し始めたあとは失敗にしない（失敗にすると CLI がフォルダを
# 戻し、プラグインは残るのにリンクだけが消えた状態になる）。~/.local/bin は消さない。
set -u

# shellcheck source=/dev/null
. "$(dirname "$0")/lib.sh"

install_dir="$(absolute_dir VW2026_HOOK_INSTALL_DIR "${VW2026_HOOK_INSTALL_DIR-}")" || exit 1
home="$(absolute_dir HOME "${HOME-}")" || exit 1
link="$home/.local/bin/vw2026"

state=none
left=""
if link_points_to "$link" "$install_dir/bin/vw2026"; then
	rm -f "$link" 2>/dev/null
	if [ -L "$link" ]; then
		state=failed
		left="$(json_string "$link")"
		printf 'cli hook: cannot remove the link %s\n' "$link" >&2
	else
		state=removed
	fi
fi

printf '{"path":%s,"left":[%s]}\n' "$(json_string "$state")" "$left"
exit 0
