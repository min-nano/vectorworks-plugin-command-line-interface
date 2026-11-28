# 検査

検査は指摘のみを行い、ファイルを書き換えません（整形は[整形](format.md)）。ツールは言語別に
標準のものを使用し、指摘の種類を過度に絞り込まず、既定に近いまま使用します。

## 実行方法

| 使用方法 | 内容 |
| --- | --- |
| `scripts/lint.sh` | 指摘の有無に関わらず全ての検査を実行し、指摘があればコード 1 で終了する。CI（`lint.yml`）はこれを実行する |

`lint.yml` は ubuntu の 1 ジョブです。OS ごとの差異は、ツールに対象の OS を渡して確認します（`go vet` の
`GOOS`）。テストは `go.yml`（Go）と `cpp.yml`（C++）が OS ごとに実行します。`lint.yml` は必須の
チェックです（実行の条件は[ビルドとリリース「CI」](plugin/build-and-release.md#ci)）。

## 言語別のツール

| 言語 | ツール | 版の固定 |
| --- | --- | --- |
| Go | `go vet`（`GOOS` を darwin・windows に切り替え、それぞれ 2026 と 2025（`-tags vw2025`。[版](plugin/editions.md)）で 4 回） | `cli/go.mod` の Go（ビルドと同じ検査ツール） |
| GitHub Actions | actionlint（`run:` の内部も shellcheck で検査する） | `tools/go.mod` |
| sh | shellcheck（shfmt が整形するのと同じ、`scripts/` と `tests/hooks/` 配下のシェルスクリプト） | `tools/requirements.txt` の `shellcheck-py`（shellcheck のリリースのバイナリを pip で配布するもの。版の読み取り方と確認方法は `scripts/common.sh` の `require_shellcheck` の注釈） |
| C++ | clang-tidy（SDK に依存しない部分。`.clang-tidy` で既定の `clang-analyzer` に `bugprone` を追加するのみとし、指摘は失敗として扱う。除外する規則と規則の設定は、理由とともに `.clang-tidy` に記述する。検査するのは、`-DVW_BUILD_PLUGIN=OFF` で構成したビルドに含まれるファイルのみで、SDK に依存するファイル（`src/*.cpp`・`src/tools/*`）は含まれない。OS ごとの実装は、その OS で構成した場合のみ含まれる。CI の `lint.yml` は ubuntu で実行されるため、mac と Windows の実装を検査しない。ローカル環境の mac で `lint.sh` を実行すると `PathCompareMac.cpp` も検査される（ローカル環境でのみ指摘が出ることがある）。Windows の `.cpp` はどの環境でも検査しない（Windows の既定の Visual Studio の生成器では `compile_commands.json` が出力されない）） | `tools/requirements.txt`（clang-format と同じ版。2 つの版が異なる場合と PATH の版が異なる場合に停止する理由は、`scripts/common.sh` の `require_clang` の注釈） |
| PowerShell | PSScriptAnalyzer（既定の規則と、整形の規則。設定は `PSScriptAnalyzerSettings.psd1`） | `scripts/powershell.ps1` の `$version` |

PSScriptAnalyzer は、同一のファイルでも時折解析器の内部で異常終了します（[PSScriptAnalyzer #1351](https://github.com/PowerShell/PSScriptAnalyzer/issues/1351)・[#1511](https://github.com/PowerShell/PSScriptAnalyzer/issues/1511)）。
そのため `scripts/powershell.ps1` の `-Mode Lint` は、ファイルごとに子プロセスの `pwsh` で検査し、解析器が異常終了した場合のみ
3 回まで再実行します（指摘があっても再実行しません）。又、最も頻繁に異常終了する `PSUseCorrectCasing` は検査から除外し、
整形の確認（`format.sh`）に委ねます。いずれも詳細は `scripts/powershell.ps1` の注釈にあります。

依存を追加する際の `govulncheck` は `CLAUDE.md` の手順で行い、ここには含めません（依存を追加する場合にのみ
実行すれば足りるため）。

## 検査の対象外

CMake・Markdown・JSON のフィクスチャは検査しません。フィクスチャは C++ と Go のテストが読み込むことで検証します。

## ローカル環境での実行

Go の他に次のものが必要です。版は CI のものに合わせます。

| 必要なもの | 導入方法 |
| --- | --- |
| clang-format・clang-tidy・shellcheck | `pip install -r tools/requirements.txt`（venv の中など。PATH に配置する） |
| CMake と C++ のコンパイラー | clang-tidy がコンパイルの指定を読み込むため |
| PowerShell（`pwsh`）と PSScriptAnalyzer | `pwsh` をインストールしてから `pwsh scripts/powershell.ps1 -Mode Install`（フックのテストに使用する Pester もインストールされる） |
