# 検査

検査は指摘することだけを行い、ファイルを書き換えません（整形は[整形](format.md)）。道具は言語ごとに
標準のものを使い、指摘の種類を絞り込みすぎず、既定に近いまま使います。

## 入口

| 使い方 | 中身 |
| --- | --- |
| `scripts/lint.sh` | すべての検査を走らせ、1 つでも指摘があれば 0 以外で終わる。CI（`lint.yml`）はこれを走らせる |

`lint.yml` は ubuntu の 1 ジョブです。OS ごとの違いは、道具に対象の OS を渡して見ます（`go vet` の
`GOOS`）。テストは `cli.yml` が OS ごとに走らせます。

## 言語ごとの道具

| 言語 | 道具 | 版の固定 | 時期 |
| --- | --- | --- | --- |
| Go | `go vet`（`GOOS` を darwin・windows・linux に替えて 3 回） | `tools/go.mod` の Go | いま |
| GitHub Actions | actionlint（`run:` の中も shellcheck で検査する） | `tools/go.mod` | いま |
| sh | shellcheck | `lint.yml` の `SHELLCHECK_VERSION`（リリースのバイナリを取る） | いま |
| C++ | clang-tidy（SDK に依らない部分。チェックは少なく始める） | clang-format と同じ版 | 段 1 |
| PowerShell | PSScriptAnalyzer | `Install-Module -RequiredVersion` | 段 1 |

依存を足すときの `govulncheck` は `CLAUDE.md` の手順で行い、ここには入れません（依存を足すときだけ
走らせればよいため）。

## 検査しないもの

CMake・Markdown・JSON の見本は検査しません。見本は C++ と Go のテストが読むことで確かめます。

## 手元で走らせる

Go のほかに shellcheck が要ります（mac は `brew install shellcheck`）。段 1 からは clang-tidy と
PowerShell（`pwsh`）も要ります。版は CI のものに合わせます。
