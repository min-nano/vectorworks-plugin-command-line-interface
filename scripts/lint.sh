#!/bin/sh
# ソースコードを実行せずに解析し、文法の誤りや不具合につながる書き方を検査する（静的解析）。
# 修正は行わない（整形は scripts/format.sh）。ツールと対象は docs/lint.md。
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

# ツールは go.mod の無いリポジトリの根では動かないので、tools/ で動かす。
# actionlint は .github/workflows を上へたどって見つけ、run: の中も shellcheck で検査する。
go -C tools tool actionlint || status=1

# shfmt が整形するのと同じファイル（scripts/ の下のシェルスクリプト）を検査する。
files="$(go -C tools tool shfmt -f "$root/scripts")"
# ファイル名に空白を含めない前提で、行ごとに引数へ分ける。
# shellcheck disable=SC2086
shellcheck $files || status=1

exit "$status"
