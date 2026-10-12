#!/bin/sh
# 検査する。直さない（整形は scripts/format.sh）。道具と対象は docs/lint.md。
set -eu

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

# OS ごとに分かれるコード（launch など）があるため、テストを走らせる OS それぞれで検査する。
for goos in darwin windows linux; do
	(cd cli && GOOS="$goos" go vet ./...)
done

# 道具は go.mod の無いリポジトリの根では動かないので、tools/ で動かす。
# actionlint は .github/workflows を上へたどって見つけ、run: の中も shellcheck で検査する。
go -C tools tool actionlint -shellcheck=shellcheck
shellcheck scripts/*.sh
