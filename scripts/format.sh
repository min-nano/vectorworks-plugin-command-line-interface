#!/bin/sh
# 整形する。検査しない（検査は scripts/lint.sh）。道具と対象は docs/format.md。
# 既定では書き換えずに確かめ、整形が要るファイルを並べて 1 で終わる（CI も同じ）。
# --fix を付けたときだけ書き換える。
set -eu

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

# 書き換えは --fix と打ったときだけにする（打ち間違いで書き換えない）。
case "$#:${1-}" in
0:) fix=false ;;
1:--fix) fix=true ;;
*)
	echo "usage: scripts/format.sh [--fix]" >&2
	exit 2
	;;
esac

# 道具は go.mod の無いリポジトリの根では動かないので、tools/ で動かして絶対パスを渡す。
shfmt() {
	go -C tools tool shfmt "$@"
}

# PATH の gofmt は手元の Go の版になるので、tools/go.mod の Go のものを使う（CI と同じ版）。
gofmt="$(go -C tools env GOROOT)/bin/gofmt"

if [ "$fix" = true ]; then
	"$gofmt" -w cli
	shfmt -w "$root/scripts"
	exit 0
fi

status=0
# gofmt -l は整形が要っても 0 で終わるので、出力で判定する。
files="$("$gofmt" -l cli)"
if [ -n "$files" ]; then
	echo "$files"
	status=1
fi
# shfmt は .editorconfig を読む（フラグで上書きしない）。整形が要れば 1 で終わる。
shfmt -l "$root/scripts" || status=1
if [ "$status" -ne 0 ]; then
	echo "run scripts/format.sh --fix to format the files above" >&2
fi
exit "$status"
