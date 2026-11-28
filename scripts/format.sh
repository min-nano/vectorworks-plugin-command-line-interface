#!/bin/sh
# ソースコードの書式（インデント・空白・改行など）を、言語ごとの標準の書式に統一する。
# 検査は行わない（検査は scripts/lint.sh）。整形ツールと整形対象は docs/format.md。
# 既定では書き換えずに確認し、整形が必要なファイルを表示してコード 1 で終了する（CI も同様）。
# --fix を指定した場合のみファイルを書き換える。
set -eu

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
# shellcheck source=scripts/common.sh
. "$root/scripts/common.sh"

# 書き換えは --fix を指定した場合のみ行う（打ち間違いで書き換えないため）。
case "$#:${1-}" in
0:) fix=false ;;
1:--fix) fix=true ;;
*)
	echo "usage: scripts/format.sh [--fix]" >&2
	exit 2
	;;
esac

# PATH 上の gofmt はローカル環境の Go の版になるため、tools/go.mod の Go の gofmt を使用する（CI と
# 同じ版）。
gofmt="$(go -C tools env GOROOT)/bin/gofmt"

require_clang clang-format
require_ruff

# 対象は git が追跡し、無視しないファイル（ビルドの出力や SDK の複製を含めない）。ファイル名に
# 空白を含めない前提で、行ごとに引数へ分割する。
cpp="$(tracked_files '*.cpp' '*.h' '*.mm')"
powershell="$(powershell_files)"
python="$(python_files)"

if [ "$fix" = true ]; then
	"$gofmt" -w cli
	shfmt_scripts -w
	# clang-format は対象が無いと標準入力を読み取るため、対象がある場合のみ実行する。
	# shellcheck disable=SC2086
	[ -z "$cpp" ] || clang-format -i $cpp
	# ruff は対象が無いとカレントディレクトリの全体を対象とするため、対象がある場合のみ実行する。
	# shellcheck disable=SC2086
	[ -z "$python" ] || ruff format $python
	# shellcheck disable=SC2086
	pwsh -NoProfile -File scripts/powershell.ps1 -Mode Fix $powershell
	exit 0
fi

status=0
# gofmt -l は整形が必要でもコード 0 で終了するため、出力の有無で判定する。
files="$("$gofmt" -l cli)"
if [ -n "$files" ]; then
	echo "$files"
	status=1
fi
# shfmt は .editorconfig の設定を読み込む（フラグで上書きしない）。整形が必要ならばコード 1 で
# 終了する。
shfmt_scripts -l || status=1
# clang-format は整形が必要なファイルを列挙しないため、1 つずつ確認して列挙する。
for file in $cpp; do
	clang-format --dry-run --Werror "$file" 2>/dev/null || {
		echo "$file"
		status=1
	}
done
# ruff format --check は整形が必要なファイルを列挙し、コード 1 で終了する。
# shellcheck disable=SC2086
[ -z "$python" ] || ruff format --check $python || status=1
# shellcheck disable=SC2086
pwsh -NoProfile -File scripts/powershell.ps1 -Mode Check $powershell || status=1
if [ "$status" -ne 0 ]; then
	echo "run scripts/format.sh --fix to format the files above" >&2
fi
exit "$status"
