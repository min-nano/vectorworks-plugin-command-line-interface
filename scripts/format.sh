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

if [ "$fix" = true ]; then
	"$gofmt" -w cli
	shfmt -w "$root/scripts"
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
if [ "$status" -ne 0 ]; then
	echo "run scripts/format.sh --fix to format the files above" >&2
fi
exit "$status"
