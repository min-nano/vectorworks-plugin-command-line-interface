#!/bin/sh
# ソースコードを静的に解析し、文法の誤りや不具合につながる書き方を検査する。
# 修正は行わない（整形は scripts/format.sh）。検査ツールと検査対象は docs/lint.md。
# 指摘の有無に関わらず全ての検査を実行し、指摘があればコード 1 で終了する。
set -eu

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

status=0

# OS ごとに分かれるコード（launch など）があるため、テストを実行する OS それぞれで検査する。
# cli/ で動かし、ビルドと同じ cli/go.mod の Go の検査ツールを使う。
for goos in darwin windows; do
	GOOS="$goos" go -C cli vet ./... || status=1
done

# 検査ツールは go.mod の無いリポジトリのルートでは動かないので、tools/ で実行する。
# actionlint は .github/workflows を上へたどって見つけ、run: の内容も shellcheck で検査する。
go -C tools tool actionlint || status=1

# shfmt が整形するのと同じファイル（scripts/ 内のシェルスクリプト）を検査する。
files="$(go -C tools tool shfmt -f "$root/scripts")"
# ファイル名に空白を含めない前提で、行ごとに引数へ分ける。
# shellcheck disable=SC2086
shellcheck $files || status=1

exit "$status"
