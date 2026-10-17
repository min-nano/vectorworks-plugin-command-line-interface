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

# C++ は SDK に依らない部分（共通部とテスト）を、CMake が書き出すコンパイルの指定で検査する。
# 指定に載るのはこの OS でビルドするファイルだけ（Windows の実装は検査しない）。clang-tidy は
# 版で指摘が変わるので、tools/requirements.txt の版でなければ止める。
want="$(sed -n 's/^clang-tidy==//p' tools/requirements.txt)"
if ! clang-tidy --version 2>/dev/null | grep -qF "version $want"; then
	echo "clang-tidy $want is required: pip install -r tools/requirements.txt" >&2
	exit 1
fi
cmake -S . -B build/lint -DVW_BUILD_PLUGIN=OFF -DVW_BUILD_TESTS=ON -DCMAKE_EXPORT_COMPILE_COMMANDS=ON >/dev/null
files="$(sed -n 's/^ *"file": "\(.*\)",*$/\1/p' build/lint/compile_commands.json)"
# shellcheck disable=SC2086
clang-tidy -p build/lint --quiet $files || status=1

# PowerShell は git が追う・無視しないファイルを検査する（format.sh と同じ対象）。
files="$(git ls-files -co --exclude-standard -- '*.ps1' '*.psm1' '*.psd1')"
# shellcheck disable=SC2086
pwsh -NoProfile -File scripts/powershell.ps1 -Mode Lint $files || status=1

exit "$status"
