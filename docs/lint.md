# 検査

検査は指摘することだけを行い、ファイルを書き換えません（整形は[整形](format.md)）。ツールは言語ごとに
標準のものを使い、指摘の種類を絞り込みすぎず、既定に近いまま使います。

## 実行方法

| 使い方 | 中身 |
| --- | --- |
| `scripts/lint.sh` | 指摘の有無に関わらず全ての検査を実行し、指摘があればコード 1 で終了する。CI（`lint.yml`）はこれを実行する |

`lint.yml` は ubuntu の 1 ジョブです。OS ごとの違いは、ツールに対象の OS を渡して見ます（`go vet` の
`GOOS`）。テストは `go.yml`（Go）と `cpp.yml`（C++）が OS ごとに実行します。`lint.yml` は必須の
チェックです（走る条件は[ビルドとリリース「CI」](plugin/build-and-release.md#ci)）。

## 言語別のツール

| 言語 | ツール | 版の固定 |
| --- | --- | --- |
| Go | `go vet`（`GOOS` を darwin・windows に替え、それぞれ 2026 と 2025（`-tags vw2025`。[版](plugin/editions.md)）で 4 回） | `cli/go.mod` の Go（ビルドと同じ検査ツール） |
| GitHub Actions | actionlint（`run:` の中も shellcheck で検査する） | `tools/go.mod` |
| sh | shellcheck（shfmt が整形するのと同じ、`scripts/` と `tests/hooks/` の下のシェルスクリプト） | `tools/requirements.txt` の `shellcheck-py`（shellcheck のリリースのバイナリを pip で配るもの。版の読み方と確かめ方は `scripts/common.sh` の `require_shellcheck` の注釈） |
| C++ | clang-tidy（SDK に依らない部分。`.clang-tidy` で既定の `clang-analyzer` に `bugprone` を足すだけにし、指摘は失敗にする。外す規則と規則の設定は、理由とともに `.clang-tidy` に書く。検査するのは、`-DVW_BUILD_PLUGIN=OFF` で構成したビルドに載るファイルだけで、SDK に依るファイル（`src/*.cpp`・`src/tools/*`）は載らない。OS ごとの実装は、その OS で構成したときだけ載る。CI の `lint.yml` は ubuntu で走るので、mac と Windows の実装を検査しない。手元の mac で `lint.sh` を走らせると `PathCompareMac.cpp` も検査される（手元でだけ指摘が出ることがある）。Windows の `.cpp` はどこでも検査しない（Windows の既定の Visual Studio の生成器では `compile_commands.json` が書き出されない）） | `tools/requirements.txt`（clang-format と同じ版。2 つの版が違うときと PATH の版が違うときに止める理由は、`scripts/common.sh` の `require_clang` の注釈） |
| PowerShell | PSScriptAnalyzer（既定の規則と、整形の規則。設定は `PSScriptAnalyzerSettings.psd1`） | `scripts/powershell.ps1` の `$version` |

PSScriptAnalyzer は、同じファイルでもときどき解析器の中で落ちます（[PSScriptAnalyzer #1351](https://github.com/PowerShell/PSScriptAnalyzer/issues/1351)・[#1511](https://github.com/PowerShell/PSScriptAnalyzer/issues/1511)）。
そのため `scripts/powershell.ps1` の `-Mode Lint` は、ファイルごとに子の `pwsh` で検査し、解析器が落ちたときだけ
3 回まで走らせ直します（指摘は走らせ直しません）。又、いちばんよく落ちる `PSUseCorrectCasing` は検査から外し、
整形の確かめ（`format.sh`）に任せます。どちらも細目は `scripts/powershell.ps1` の注釈にあります。

依存を足すときの `govulncheck` は `CLAUDE.md` の手順で行い、ここには入れません（依存を足すときだけ
実行すれば良いため）。

## 検査の対象外

CMake・Markdown・JSON の見本は検査しません。見本は C++ と Go のテストが読むことで確かめます。

## ローカル環境での実行

Go の他に次が要ります。版は CI のものに合わせます。

| 要るもの | 入れ方 |
| --- | --- |
| clang-format・clang-tidy・shellcheck | `pip install -r tools/requirements.txt`（venv の中など。PATH に置く） |
| CMake と C++ のコンパイラー | clang-tidy がコンパイルの指定を読むため |
| PowerShell（`pwsh`）と PSScriptAnalyzer | `pwsh` を入れてから `pwsh scripts/powershell.ps1 -Mode Install`（フックのテストに使う Pester も入る） |
