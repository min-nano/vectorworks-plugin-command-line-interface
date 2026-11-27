# 検査

検査は指摘することだけを行い、ファイルを書き換えません（整形は[整形](format.md)）。ツールは言語ごとに
標準のものを使い、指摘の種類を絞り込みすぎず、既定に近いまま使います。

## 入口

| 使い方 | 中身 |
| --- | --- |
| `scripts/lint.sh` | 指摘の有無に関わらず全ての検査を実行し、指摘があればコード 1 で終了する。CI（`lint.yml`）はこれを実行する |

`lint.yml` は ubuntu の 1 ジョブです。OS ごとの違いは、ツールに対象の OS を渡して見ます（`go vet` の
`GOOS`）。テストは `go.yml`（Go）と `cpp.yml`（C++）が OS ごとに実行します。`lint.yml` は必須の
チェックです（走る条件は[ビルドとリリース「CI」](plugin/build-and-release.md#ci)）。

## 言語ごとのツール

| 言語 | ツール | 版の固定 |
| --- | --- | --- |
| Go | `go vet`（`GOOS` を darwin・windows に替え、それぞれ 2026 と 2025（`-tags vw2025`。[版](plugin/editions.md)）で 4 回） | `cli/go.mod` の Go（ビルドと同じ検査ツール） |
| GitHub Actions | actionlint（`run:` の中も shellcheck で検査する） | `tools/go.mod` |
| sh | shellcheck（shfmt が整形するのと同じ、`scripts/` と `tests/hooks/` の下のシェルスクリプト） | `tools/requirements.txt` の `shellcheck-py`（shellcheck のリリースのバイナリを pip で配るもの。第三者が包み直したもので、版は `<shellcheck の版>.<包み直しの番号>`。`lint.sh` は、PATH の shellcheck の版が、最後の番号を除いた版と違えば止める。actionlint も PATH の shellcheck を使う） |
| C++ | clang-tidy（SDK に依らない部分。`.clang-tidy` で既定の `clang-analyzer` に `bugprone` を足すだけにし、指摘は失敗にする。外す規則と規則の設定は、理由とともに `.clang-tidy` に書く。CMake が書き出すコンパイルの指定（`build/lint/compile_commands.json`。プラグインを外して `-DVW_BUILD_PLUGIN=OFF` で構成する）に載るファイルを検査するので、検査するのは**その構成のビルドに載るファイル**で、SDK に依るファイル（`src/*.cpp`・`src/tools/*`）は載らない。OS ごとの実装（`FilesWindows.cpp`・`WindowsPath.cpp`・`SaveTargetWindows.cpp`・`PathCompareMac.cpp`・`PathCompareWindows.cpp`）は、その OS で構成したときだけ載る。CI の `lint.yml` は ubuntu で走るのでこれらを検査しない。手元の mac で `lint.sh` を走らせると `PathCompareMac.cpp` も検査される（手元でだけ指摘が出ることがある）。Windows の `.cpp` はどこでも検査しない（`lint.sh` は sh のスクリプトで、生成器を指定しないので、Windows の既定の Visual Studio の生成器では `compile_commands.json` が書き出されない）） | `tools/requirements.txt`（clang-format と同じ版。`format.sh`・`lint.sh` は、2 つの版が違えば止める。Dependabot は版の出る時期の違いで片方だけを上げることがあるため。`lint.sh` は PATH の版が違っても止める） |
| PowerShell | PSScriptAnalyzer（既定の規則と、整形の規則。設定は `PSScriptAnalyzerSettings.psd1`） | `scripts/powershell.ps1` の `$version` |

PSScriptAnalyzer は、同じファイルでもときどき解析器の中で落ちます（`NullReferenceException`、PowerShell の実行時の
「An item with the same key has already been added」でプロセスごと終わるもの。指摘ではなく、走らせ直すと通る。
[PSScriptAnalyzer #1351](https://github.com/PowerShell/PSScriptAnalyzer/issues/1351)・[#1511](https://github.com/PowerShell/PSScriptAnalyzer/issues/1511)）。
プロセスごと落ちるとスクリプトの中では受け止められないので、`scripts/powershell.ps1` の `-Mode Lint` は、ファイルごとに
子の `pwsh`（`-Mode LintFile`）で検査し、子が指摘（1）でも通る（0）でもなく終わったときだけ、どのファイルで落ちたかを
標準エラーに書いてから 3 回まで走らせ直します。指摘は走らせ直しません。

- 子は、落ちたときに `exit` ではなく `[Environment]::Exit(3)` で終わります。`exit` は後片付けで、落ちた解析器の
  まだ動いているスレッドを待ち、15 分ほど戻らないことがあったためです。
- **`PSUseCorrectCasing` は検査では外します**（`-ExcludeRule`）。いちばんよく落ちる規則（コマンドの情報を引く
  ところの `NullReferenceException`）で、整形（`Invoke-Formatter`）が同じ設定で当てる規則なので、大文字と小文字の
  違いは `format.sh` の確かめが見つけます（検査から外しても、見つけるものは減らない）。

依存を足すときの `govulncheck` は `CLAUDE.md` の手順で行い、ここには入れません（依存を足すときだけ
実行すればよいため）。

## 検査しないもの

CMake・Markdown・JSON の見本は検査しません。見本は C++ と Go のテストが読むことで確かめます。

## 手元で実行する

Go のほかに次が要ります。版は CI のものに合わせます。

| 要るもの | 入れ方 |
| --- | --- |
| clang-format・clang-tidy・shellcheck | `pip install -r tools/requirements.txt`（venv の中など。PATH に置く） |
| CMake と C++ のコンパイラ | clang-tidy がコンパイルの指定を読むため |
| PowerShell（`pwsh`）と PSScriptAnalyzer | `pwsh` を入れてから `pwsh scripts/powershell.ps1 -Mode Install`（フックのテストに使う Pester も入る） |
