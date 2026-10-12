#!/bin/sh
# 整形する。--check を付けると書き換えず、整形が要るファイルを並べて 1 で終わる（CI）。
# 道具と対象は docs/format.md。
set -eu

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

case "$#:${1-}" in
0:) check=false ;;
1:--check) check=true ;;
*)
	echo "usage: scripts/format.sh [--check]" >&2
	exit 2
	;;
esac

# 道具は go.mod の無いリポジトリの根では動かないので、tools/ で動かして絶対パスを渡す。
shfmt() {
	go -C tools tool shfmt "$@"
}

# PATH の gofmt は手元の Go の版になるので、tools/go.mod の Go のものを使う（CI と同じ版）。
gofmt="$(go -C tools env GOROOT)/bin/gofmt"

if [ "$check" = true ]; then
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
		echo "run scripts/format.sh to format the files above" >&2
	fi
	exit "$status"
fi

"$gofmt" -w cli
shfmt -w "$root/scripts"
