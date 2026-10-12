# 整形

複数の言語が同じリポジトリに入るので、**整形の決まりを 1 か所に集め、言語ごとのツールは標準のものを
ほぼ既定のまま使います。** 整形は直すことだけを行い、指摘は[検査](lint.md)が行います。

## 入口

| 使い方 | 中身 |
| --- | --- |
| `scripts/format.sh` | 書き換えずに、整形が要るファイルを並べて 1 で終わる。CI（`format.yml`）はこれを走らせる |
| `scripts/format.sh --fix` | 対象のファイルを書き換えて整形する |

既定は確かめるだけにし、書き換えるのは `--fix` と打ったときだけにします。これ以外の引数は使い方を
出して 2 で終わります（打ち間違いで書き換えないため）。
`format.yml` は必須のチェックなので、変えたパスで絞らず、すべての push（main）・PR・マージキューで
走ります（[CI](plugin/build-and-release.md#ci)）。

手元と CI は同じスクリプトを走らせます。ツールの版は固定し（下記）、手元と CI で結果を食い違わせません。

## 言語をまたぐ決まり

`.editorconfig` に書きます。各言語の設定ファイルには、その言語にしかない項目だけを書きます。
整形しないもの（下記）には `.editorconfig` でも決まりを当てず（`unset`）、エディタに中身を変えさせません。

| 項目 | 決まり | 例外 |
| --- | --- | --- |
| 文字コード | UTF-8（BOM なし） | `.vwstrings` は UTF-16LE（BOM 付き）。PowerShell（`.ps1`・`.psm1`・`.psd1`）は UTF-8（BOM 付き。Windows PowerShell 5.1 は BOM の無いファイルを ANSI のコードページで読み、日本語を読み崩すため） |
| 改行 | LF。`.gitattributes` でも LF にそろえる | `.vwstrings` は CRLF。git に変換させない（`-text`） |
| 末尾 | 改行で終える。行末の空白を残さない | |
| インデント | **タブ（表示幅 4）** | YAML（タブを使えない）と Markdown はスペース 2 |
| 1 行の長さ | 100 桁を目安にする。強制するのは C++（clang-format）だけ | Markdown は制限しない |

インデントをタブにするのは、Go（gofmt）が必須とし、shfmt の既定でもあり、clang-format と
PSScriptAnalyzer も選べるためです。これで YAML と Markdown を除くすべての言語がそろいます。

`.editorconfig` で表せない決まりは次のとおりです。

- **ブレース**: 開き括弧を同じ行に置きます（gofmt・shfmt の形）。C++ は `.clang-format` で同じ形にします。
- **コメントの位置**: 真実は `CLAUDE.md`「整形と検査」です。ツールでは強制できないので、レビューで確かめます。

## 言語ごとのツール

| 言語 | ツール | 設定 | 版の固定 | 時期 |
| --- | --- | --- | --- | --- |
| Go | gofmt | なし（設定できない） | `tools/go.mod` の Go の gofmt（PATH のものは使わない） | いま |
| sh | shfmt | `.editorconfig` を読む。フラグで上書きしない | `tools/go.mod` | いま |
| C++ / Objective-C++ | clang-format | `.clang-format`（LLVM の既定に `UseTab: ForIndentation`・`IndentWidth`・`TabWidth`・`ColumnLimit: 100`・`AccessModifierOffset` だけを足す。ブレースは既定の `Attach`） | `tools/requirements.txt`（pip の `clang-format==X`。3 OS で同じ出力。`format.sh` は PATH の版が違えば止める） | いま |
| PowerShell | `Invoke-Formatter`（PSScriptAnalyzer） | `PSScriptAnalyzerSettings.psd1`（`CodeFormatting` と同じ規則で、`PSUseConsistentIndentation` の `Kind = 'tab'`） | `scripts/powershell.ps1` の `$version`（`Install-Module -RequiredVersion`。`-Mode Install` で入る） | いま |

対象は、Go は `cli/`、sh は `scripts/`、C++ と PowerShell は git が追う・無視しないファイル
（`git ls-files -co --exclude-standard`。ビルドの出力や SDK の写しを含めない）です。PowerShell の
整形と検査は `scripts/powershell.ps1` を通して行います（PSScriptAnalyzer を呼ぶための入口で、
`format.sh` と `lint.sh` が呼ぶ）。

Go で書かれたツールは `tools/go.mod` に `tool` で書き、`go -C tools tool <名前>` で呼びます。Go は
すでに要るので、ほかのパッケージ管理を足さずに済みます。CLI の `go.mod` とは分けるので、
`vw2026` には入りません。版は dependabot が上げます。

`tools/go.mod` が求める Go が手元や CI の Go より新しければ、`go` がその版を取ってきて切り替えます
（`GOTOOLCHAIN=auto`。CI でも同じ指定で動かす）。手元の Go のほうが新しければ手元の版で動きます。
新しい版のほうが厳しくなる向きなので、版を下げて揃えることはしません。

## 整形しないもの

| 対象 | 理由 |
| --- | --- |
| CMake | cmake-format（cmakelang）は保守が止まっている。`CMakeLists.txt` は 1 つだけで、`.editorconfig` の決まりで足りる |
| Markdown | 整形ツールが日本語の段落と長い表をうまく扱えない |
| YAML | actionlint の検査で足りる（[検査](lint.md)） |
| `protocol/fixtures/` の JSON | 壊れた要求をわざと含む。整形すると見本の意味が変わる |
| `.vwstrings` | Vectorworks が読む形のまま保つ |

## ツールを足すとき

- 言語が入る段で、その言語のツールと設定を `scripts/format.sh` と上の表に足します。
- 設定は既定から変える項目だけを書き、変える理由を先頭の注釈に書きます。上の「言語をまたぐ決まり」
  と食い違う設定は入れません。
- ツールを Go のモジュールで足すときも、`CLAUDE.md` の依存の確認を行います（推移的な依存の数は問わない）。
