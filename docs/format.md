# 整形

複数の言語が同じリポジトリに入るので、**整形の決まりを 1 か所に集め、言語ごとの道具は標準のものを
ほぼ既定のまま使います。** 整形は直すことだけを行い、指摘は[検査](lint.md)が行います。

## 入口

| 使い方 | 中身 |
| --- | --- |
| `scripts/format.sh` | 対象のファイルを書き換えて整形する |
| `scripts/format.sh --check` | 書き換えずに、整形が要るファイルを並べて 1 で終わる。CI（`format.yml`）はこれを走らせる |

手元と CI は同じスクリプトを走らせます。道具の版は固定し（下記）、手元と CI で結果を食い違わせません。

## 言語をまたぐ決まり

`.editorconfig` に書きます。各言語の設定ファイルには、その言語にしかない項目だけを書きます。

| 項目 | 決まり | 例外 |
| --- | --- | --- |
| 文字コード | UTF-8（BOM なし） | `.vwstrings` は UTF-16LE（BOM 付き） |
| 改行 | LF。`.gitattributes` でも LF にそろえる | `.vwstrings` は CRLF。git に変換させない（`-text`） |
| 末尾 | 改行で終える。行末の空白を残さない | |
| インデント | **タブ（表示幅 4）** | YAML（タブを使えない）と Markdown はスペース 2 |
| 1 行の長さ | 100 桁を目安にする。強制するのは C++（clang-format）だけ | Markdown は制限しない |
| ブレース | 開き括弧を同じ行に置く（Go・shfmt の形） | |
| コメント | 対象の行の**直前**に置き、行末には書かない | |

インデントをタブにするのは、Go（gofmt）が必須とし、shfmt の既定でもあり、clang-format と
PSScriptAnalyzer も選べるためです。これで YAML と Markdown を除くすべての言語がそろいます。

コメントの位置は道具では強制できないので、レビューで確かめます。

## 言語ごとの道具

| 言語 | 道具 | 設定 | 版の固定 | 時期 |
| --- | --- | --- | --- | --- |
| Go | gofmt | なし（設定できない） | `tools/go.mod` の Go | いま |
| sh | shfmt | `.editorconfig` を読む。フラグで上書きしない | `tools/go.mod` | いま |
| C++ / Objective-C++ | clang-format | `.clang-format`（`UseTab: ForIndentation`・`ColumnLimit: 100`・`BreakBeforeBraces: Attach`） | pip の `clang-format==X`（3 OS で同じ出力） | 段 1 |
| PowerShell | `Invoke-Formatter`（PSScriptAnalyzer） | `PSScriptAnalyzerSettings.psd1`（`PSUseConsistentIndentation` の `Kind = 'tab'`） | `Install-Module -RequiredVersion` | 段 1 |

Go で書かれた道具は `tools/go.mod` に `tool` で書き、`go -C tools tool <名前>` で呼びます。Go は
すでに要るので、ほかのパッケージ管理を足さずに済みます。CLI の `go.mod` とは分けるので、
`vw2026` には入りません。版は dependabot が上げます。

## 整形しないもの

| 対象 | 理由 |
| --- | --- |
| CMake | 元のリポジトリの cmake-format（cmakelang）は保守が止まっている。`CMakeLists.txt` は 1 つだけで、`.editorconfig` の決まりで足りる |
| Markdown | 整形の道具が日本語の段落と長い表をうまく扱えない |
| YAML | actionlint の検査で足りる（[検査](lint.md)） |
| `protocol/fixtures/` の JSON | 壊れた要求をわざと含む。整形すると見本の意味が変わる |
| `.vwstrings` | Vectorworks が読む形のまま保つ |

## 道具を足すとき

- 言語が入る段で、その言語の道具と設定を `scripts/format.sh` と上の表に足します。
- 設定は既定から変える項目だけを書き、変える理由を先頭の注釈に書きます。上の「言語をまたぐ決まり」
  と食い違う設定は入れません。
- 道具を Go のモジュールで足すときも、`CLAUDE.md` の依存の確認（保守・ライセンス・脆弱性・版の固定）
  を行います。実行ファイルには入らないので、推移的な依存の数は問いません。
