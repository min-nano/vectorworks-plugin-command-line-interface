#!/bin/sh
# ソースコードの書式（インデント・空白・改行など）を、言語ごとの標準の書式に揃える。
# 検査は行わない（検査は scripts/lint.sh）。整形ツールと整形対象は docs/format.md。
# 既定では書き換えずに確認し、整形が必要なファイルを表示してコード 1 で終了する（CI も同じ）。
# --fix を指定したときだけファイルを書き換える。
set -eu

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

# 書き換えは --fix を指定したときだけ行う（打ち間違いで書き換えないため）。
case "$#:${1-}" in
0:) fix=false ;;
1:--fix) fix=true ;;
*)
	echo "usage: scripts/format.sh [--fix]" >&2
	exit 2
	;;
esac

# 整形ツールは go.mod の無いリポジトリのルートでは動かないので、tools/ で実行し、絶対パスを渡す。
shfmt() {
	go -C tools tool shfmt "$@"
}

# PATH 上の gofmt は手元の Go の版になるので、tools/go.mod の Go の gofmt を使う（CI と同じ版）。
gofmt="$(go -C tools env GOROOT)/bin/gofmt"

# clang-format は版で出力が変わるので、tools/requirements.txt の版でなければ止める。
want="$(sed -n 's/^clang-format==//p' tools/requirements.txt)"
if ! clang-format --version 2>/dev/null | grep -qF "version $want"; then
	echo "clang-format $want is required: pip install -r tools/requirements.txt" >&2
	exit 1
fi

# PowerShell の整形は pwsh で行うので、無ければ何も整形・確認せずに止める（途中で失敗させない）。
if ! command -v pwsh >/dev/null 2>&1; then
	echo "pwsh is required: see docs/lint.md" >&2
	exit 1
fi

# 対象は git が追う・無視しないファイル（ビルドの出力や SDK の写しを含めない）。ファイル名に
# 空白を含めない前提で、行ごとに引数へ分ける。
cpp="$(git ls-files -co --exclude-standard -- '*.cpp' '*.h' '*.mm')"
powershell="$(git ls-files -co --exclude-standard -- '*.ps1' '*.psm1' '*.psd1')"

if [ "$fix" = true ]; then
	"$gofmt" -w cli
	shfmt -w "$root/scripts"
	# clang-format は対象が無いと標準入力を読むので、対象があるときだけ走らせる。
	# shellcheck disable=SC2086
	[ -z "$cpp" ] || clang-format -i $cpp
	# shellcheck disable=SC2086
	pwsh -NoProfile -File scripts/powershell.ps1 -Mode Fix $powershell
	exit 0
fi

status=0
# gofmt -l は整形が必要でもコード 0 で終了するので、出力の有無で判定する。
files="$("$gofmt" -l cli)"
if [ -n "$files" ]; then
	echo "$files"
	status=1
fi
# shfmt は .editorconfig の設定を読む（フラグで上書きしない）。整形が必要ならコード 1 で終了する。
shfmt -l "$root/scripts" || status=1
# clang-format は整形が必要なファイルを並べないので、1 つずつ確かめて並べる。
for file in $cpp; do
	clang-format --dry-run --Werror "$file" 2>/dev/null || {
		echo "$file"
		status=1
	}
done
# shellcheck disable=SC2086
pwsh -NoProfile -File scripts/powershell.ps1 -Mode Check $powershell || status=1
if [ "$status" -ne 0 ]; then
	echo "run scripts/format.sh --fix to format the files above" >&2
fi
exit "$status"
