# 整形

複数の言語が同一のリポジトリに含まれるため、**整形の規則を 1 か所に集約し、言語別のツールは標準のものを
ほぼ既定のまま使用します。** 整形は修正のみを行い、指摘は[検査](lint.md)が行います。

## 実行方法

| 使用方法 | 内容 |
| --- | --- |
| `scripts/format.sh` | 書き換えずに、整形が必要なファイルを列挙してコード 1 で終了する。CI（`format.yml`）はこれを実行する |
| `scripts/format.sh --fix` | 対象のファイルを書き換えて整形する |

既定は確認のみとし、書き換えは `--fix` を指定した場合に限ります。これ以外の引数は使用方法を
出力してコード 2 で終了します。
`format.yml` は必須のチェックです（実行の条件は[ビルドとリリース「CI」](plugin/build-and-release.md#ci)）。

ローカル環境と CI は同一のスクリプトを実行します。ツールの版は固定し（下記）、ローカル環境と CI の間で結果を
相違させません。

## 言語共通の規則

`.editorconfig` に記述します。各言語の設定ファイルには、その言語固有の項目のみを記述します。
整形の対象外（下記）には `.editorconfig` でも規則を適用せず（`unset`）、エディタに内容を変更させません。

| 項目 | 規則 | 例外 |
| --- | --- | --- |
| 文字コード | UTF-8（BOM なし） | `.vwstrings` は UTF-16LE（BOM 付き）。PowerShell（`.ps1`・`.psm1`・`.psd1`）は UTF-8（BOM 付き。Windows PowerShell 5.1 は BOM の無いファイルを ANSI のコードページで読み込み、日本語を誤って解釈するため） |
| 改行 | LF。`.gitattributes` でも LF に統一する | `.vwstrings` は CRLF。git に変換させない（`-text`） |
| 末尾 | 改行で終了する。行末に空白を含めない | |
| インデント | **タブ（表示幅 4）** | YAML（タブを使用できない）と Markdown はスペース 2 |
| 1 行の長さ | 100 桁を目安とする。強制するのは C++（clang-format）と Python（ruff）のみ | Markdown は制限しない |

インデントをタブとするのは、Go（gofmt）が必須とし、shfmt の既定でもあり、clang-format と
PSScriptAnalyzer でも選択できるためです。これにより YAML と Markdown を除く全ての言語が統一されます。

`.editorconfig` で表現できない規則は次の通りです。

- **ブレース**: 開き括弧を同じ行に配置します（gofmt・shfmt の形式）。C++ は `.clang-format` で同じ形式にします。
- **コメントの位置**: 真実は `CLAUDE.md`「整形と検査」です。ツールでは強制できないため、レビューで確認します。

## 言語別のツール

| 言語 | ツール | 設定 | 版の固定 |
| --- | --- | --- | --- |
| Go | gofmt | なし（設定できない） | `tools/go.mod` の Go の gofmt（PATH のものは使用しない） |
| sh | shfmt | `.editorconfig` を読み込む。フラグで上書きしない | `tools/go.mod` |
| C++ / Objective-C++ | clang-format | `.clang-format`（LLVM の既定から変更する項目と理由は、その先頭の注釈。ブレースは既定の `Attach`） | `tools/requirements.txt`（pip の `clang-format==X`。3 OS で同一の出力。clang-tidy と同じ版にする。[検査](lint.md)） |
| Python | `ruff format` | `ruff.toml`（ruff は `.editorconfig` を読み込まないため、既定と異なるタブと 100 桁のみを記述する） | `tools/requirements.txt`（pip の `ruff==X`） |
| PowerShell | `Invoke-Formatter`（PSScriptAnalyzer） | `PSScriptAnalyzerSettings.psd1`（`CodeFormatting` と同じ規則で、インデントのみタブ） | `scripts/powershell.ps1` の `$version`（`-Mode Install` でインストールされる） |

対象は、Go は `cli/`、sh は `scripts/` と `tests/hooks/`、C++・Python・PowerShell は git が追跡し、無視しないファイル
（ビルドの出力や SDK の複製を含めない）です。PowerShell の整形と検査は `scripts/powershell.ps1` を介して
行います（`format.sh` と `lint.sh` が呼び出す。フックのテストに使用する Pester の版もここで定める）。

Go で書かれたツールは `tools/go.mod` に `tool` で記述し、`go -C tools tool <名前>` で呼び出します。Go は
既に必要であるため、他のパッケージ管理を追加する必要がありません。CLI の `go.mod` とは分離するため、
`vw2026` には含まれません。版は dependabot が更新します。

`tools/go.mod` が要求する Go がローカル環境や CI の Go より新しい場合は、`go` がその版を取得して切り替えます
（`GOTOOLCHAIN=auto`。CI でも同じ指定で実行する）。ローカル環境の Go の方が新しい場合はローカル環境の版で
動作します。新しい版の方が厳格になる傾向があるため、版を下げて統一することはしません。

## 整形の対象外

| 対象 | 理由 |
| --- | --- |
| CMake | cmake-format（cmakelang）は保守が停止している。`CMakeLists.txt` は 1 つのみで、`.editorconfig` の規則で足りる |
| Markdown | 整形ツールが日本語の段落と長い表を適切に扱えない |
| YAML | actionlint の検査で足りる（[検査](lint.md)） |
| `protocol/fixtures/` の JSON | 壊れた要求を意図的に含む。整形するとフィクスチャの意味が変化する |
| `.vwstrings` | Vectorworks が読み込む形式のまま維持する |

## ツールの追加

- 言語を追加する際は、その言語のツールと設定を `scripts/format.sh` と上の表に追加します。
- 設定は既定から変更する項目のみを記述し、変更の理由を先頭の注釈に記述します。上の「言語共通の規則」
  と矛盾する設定は導入しません。
- ツールを Go のモジュールで追加する場合も、`CLAUDE.md`「Go」の手順で依存を確認します（推移的な依存の数は問わない）。
