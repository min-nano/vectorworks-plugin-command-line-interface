#!/bin/sh
# 検査する。直さない（整形は scripts/format.sh）。道具と対象は docs/lint.md。
# 1 つが指摘を出しても残りを走らせ、指摘があれば最後に 1 で終わる。
set -eu

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

status=0

# OS ごとに分かれるコード（launch など）があるため、テストを走らせる OS それぞれで検査する。
# cli/ で動かし、ビルドと同じ cli/go.mod の Go の検査器を使う。
for goos in darwin windows linux; do
	GOOS="$goos" go -C cli vet ./... || status=1
done

# 道具は go.mod の無いリポジトリの根では動かないので、tools/ で動かす。
# actionlint は .github/workflows を上へたどって見つけ、run: の中も shellcheck で検査する。
go -C tools tool actionlint || status=1

# shfmt が整形するのと同じファイル（scripts/ の下のシェルスクリプト）を検査する。
files="$(go -C tools tool shfmt -f "$root/scripts")"
# ファイル名に空白を含めない前提で、行ごとに引数へ分ける。
# shellcheck disable=SC2086
shellcheck $files || status=1

exit "$status"
