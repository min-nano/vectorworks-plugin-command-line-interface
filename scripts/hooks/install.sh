#!/bin/sh
# cli の install のフック（mac）。~/.local/bin/vw2026 から <Plug-Ins>/cli/bin/vw2026 へのリンクを置き
# （2025 の CLI が走らせたときは vw2025。lib.sh の hook_year）、
# 標準出力に {"path":…} を 1 つだけ書く（docs/plugin/install-and-update.md「PATH」）。
#
#   linked       リンクを作った（~/.local/bin は PATH にある）
#   exists       このプラグインの CLI を指すリンクが既にあった（~/.local/bin は PATH にある）
#   not_in_path  リンクはある（作った・既にあった）が、~/.local/bin が PATH に無い
#   conflict     ~/.local/bin/vw2026 か ~/.local/bin に別のもの（利用者のファイル・別の場所を指すリンク）がある
#   failed       リンクを作れなかった
#
# 何度走らせても同じ結果になる。PATH の失敗ではフックを失敗にしない（プラグインは動く）。失敗に
# するのは、何も変える前の確かめ（環境変数・CLI があること）だけ。シェルの設定ファイルは書き換えない。
set -u

# shellcheck source=SCRIPTDIR/lib.sh
. "$(dirname "$0")/lib.sh"

hook_init
target="$install_dir/bin/$command"
if [ ! -f "$target" ]; then
	hook_fail "the CLI is missing: $target"
fi
bin_dir="$home/.local/bin"
link="$bin_dir/$command"

if link_points_to "$link" "$target"; then
	state=exists
elif [ -L "$link" ] || [ -e "$link" ]; then
	state=conflict
	printf 'cli hook: %s is not a link to %s; left it as it is\n' "$link" "$target" >&2
elif [ -e "$bin_dir" ] && [ ! -d "$bin_dir" ]; then
	state=conflict
	printf 'cli hook: %s is not a directory; left it as it is\n' "$bin_dir" >&2
elif mkdir -p "$bin_dir" && ln -s "$target" "$link"; then
	state=linked
else
	state=failed
	printf 'cli hook: cannot make the link %s\n' "$link" >&2
fi

if { [ "$state" = linked ] || [ "$state" = exists ]; } && ! dir_in_path "$bin_dir"; then
	state=not_in_path
	printf 'cli hook: %s is not in PATH. Add it in the profile of your shell, for example\n' "$bin_dir" >&2
	# shellcheck disable=SC2016
	printf '\techo '\''export PATH="$HOME/.local/bin:$PATH"'\'' >> ~/.zprofile\n' >&2
	printf 'and open a new terminal.\n' >&2
fi

printf '{"path":%s}\n' "$(json_string "$state")"
