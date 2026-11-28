#!/bin/sh
# cli の uninstall のフック（mac）。~/.local/bin/vw2026 が <Plug-Ins>/cli/bin/vw2026 を指すリンク
# （2025 の CLI が実行した場合は vw2025。lib.sh の hook_year。同じディレクトリを別の表記で指すものも
# 含む。lib.sh の link_points_to）の場合のみ削除し、標準出力に {"path":…,"left":[…]} を 1 つのみ
# 書き込む（docs/plugin/install-and-update.md「`uninstall` のフック」）。
#
#   removed  リンクを削除した
#   none     このプラグインの CLI を指すリンクが無かった（利用者のファイル・別の場所を指す
#            リンクには触れない）
#   failed   リンクを削除できなかった（left に記載する）
#
# フォルダは <Work>/old/cli/ へ移動した後で実行される（VW2026_HOOK_PLUGIN_DIR は移動先）ため、
# 削除するものは VW2026_HOOK_INSTALL_DIR から決定する。削除を開始した後は失敗させない（失敗させると
# CLI がフォルダを元に戻し、プラグインは残存するのにリンクのみが削除された状態になる）。
# ~/.local/bin は削除しない。
set -u

# shellcheck source=SCRIPTDIR/lib.sh
. "$(dirname "$0")/lib.sh"

hook_init
link="$home/.local/bin/$command"

state=none
left=""
if link_points_to "$link" "$install_dir/bin/$command"; then
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
