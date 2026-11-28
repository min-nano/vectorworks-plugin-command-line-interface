#!/bin/sh
# ソースコードを静的に解析し、文法の誤りや不具合につながる記述を検査する。
# 修正は行わない（整形は scripts/format.sh）。検査ツールと検査対象は docs/lint.md。
# 指摘の有無に関わらず全ての検査を実行し、指摘があればコード 1 で終了する。
set -eu

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
# shellcheck source=scripts/common.sh
. "$root/scripts/common.sh"

status=0

# OS ごとに分かれるコード（launch など）があるため、テストを実行する各 OS で検査する。版ごとに
# 分かれるコード（ビルドタグ vw2025。docs/plugin/editions.md）も、2026 と 2025 の両方で検査する。
# cli/ で実行し、ビルドと同じ cli/go.mod の Go の検査ツールを使用する。
for goos in darwin windows; do
	GOOS="$goos" go -C cli vet ./... || status=1
	GOOS="$goos" go -C cli vet -tags vw2025 ./... || status=1
done

# actionlint と、下の shellcheck の検査が使用する shellcheck の版を確認する。
require_shellcheck

# 検査ツールは go.mod の無いリポジトリのルートでは動作しないため、tools/ で実行する。
# actionlint は .github/workflows を上へ遡って検出し、run: の内容も shellcheck で検査する。
go -C tools tool actionlint || status=1

# shfmt が整形するのと同じファイル（scripts/ と tests/hooks/ 内のシェルスクリプト）を検査する。
files="$(shfmt_scripts -f)"
# ファイル名に空白を含めない前提で、行ごとに引数へ分割する。
# shellcheck disable=SC2086
shellcheck $files || status=1

# C++ は SDK に依存しない部分（共通部とテスト）を、CMake が書き出すコンパイルの指定で検査する。
# 指定に含まれるのはこの OS でビルドするファイルのみ（Windows の実装は検査しない）。
require_clang clang-tidy
cmake -S . -B build/lint -DVW_BUILD_PLUGIN=OFF -DVW_BUILD_TESTS=ON -DCMAKE_EXPORT_COMPILE_COMMANDS=ON >/dev/null
files="$(sed -n 's/^ *"file": "\(.*\)",*$/\1/p' build/lint/compile_commands.json)"
# shellcheck disable=SC2086
clang-tidy -p build/lint --quiet $files || status=1

# PowerShell は git が追跡し、無視しないファイルを検査する（format.sh と同じ対象）。
files="$(powershell_files)"
# shellcheck disable=SC2086
pwsh -NoProfile -File scripts/powershell.ps1 -Mode Lint $files || status=1

exit "$status"
