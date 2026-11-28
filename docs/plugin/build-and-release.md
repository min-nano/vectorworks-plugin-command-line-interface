# ビルドとリリース

SDK のリンクとバンドルの構成は、SDK リファレンスの実機確認プラグイン（[`plugin/`](https://github.com/min-nano/vectorworks-developer-sdk-reference/tree/main/plugin)）の `CMakeLists.txt` で実機に読み込まれている構成に準拠します（[プラグインの設計「根拠」](README.md#根拠)）。
ビルドの設定の詳細は `CMakeLists.txt` と各ワークフローの注釈に記載されており、本ページは構成と決定事項を記述します。

## CMake

| 項目 | 内容 |
| --- | --- |
| プロジェクト | `project(VwCli CXX)`（Apple では OBJCXX も）。CMake は 3.24 以上（[CMake の版](#cmake-の版)） |
| 選択肢 | `VW_BUILD_PLUGIN`（ON）・`VW_BUILD_TESTS`（OFF）・`VW_BUILD_FUZZERS`（OFF。clang の libFuzzer が必要）・`VW_ENABLE_SANITIZERS`（OFF）・`VW_ENABLE_COVERAGE`（OFF）・`VW_ENABLE_PCH`（ON）。`VW_BUILD_PLUGIN` が ON で `VW_SDK_DIR` が無い場合は構成の段階で停止する（暗黙にプラグインを除外しない）。SDK の無い環境（テスト・検査・ファジング。ubuntu を含む）では `-DVW_BUILD_PLUGIN=OFF` を指定する |
| SDK | `VW_SDK_DIR`（`SDKLib` を含む）。mac は `libVWSDK.a`＋VWMM・`BuildVWR`、Windows は x64 のみ（`VWSDK.lib`・`VWMM.lib`・`buildvwr.exe`）。いずれかが無い場合は構成の段階で停止する。配布するプラグイン（CI）は Release でビルドする。SDK の Release のライブラリに合わせる指定（`RELEASE_BLD`・Windows の `/MD`）は構成に依らず付与されるので、[ローカル環境でのビルド](#ローカル環境でのビルド)の `RelWithDebInfo` でも SDK と齟齬が生じない |
| 警告 | SDK に依存しないターゲット（共通部・テスト・ファジング）にのみ付与し、プラグインには付与しない |
| 共通部 | `VwCliCore`（STATIC。`src/core/*`。SDK に依存しない。プラグインとテストがリンクする）。OS 別の実装は当該 OS の分のみを含める（[保存先の検証「実装とテストの分担」](save-target.md#実装とテストの分担)） |
| テスト | `VwCliTests`（`tests/*Tests.cpp`。外部のテストの枠組みに依存しない）・`JsonFuzzer`（`tests/JsonFuzzer.cpp`）。テストは `protocol/fixtures` を読み込む |
| プラグイン | `add_library(<target> MODULE)`。`VW_PLUGIN_SOURCES`（`src/*.cpp`・`src/tools/*`） |
| 刻印 | `VW_BUILD_VERSION`（短い sha）・`VW_BUILD_BRANCH`。PR では、ブランチの head の sha とブランチの名前（[リリースの構成](#リリースの構成)）。CI では `meta` が決定し、ビルドのジョブが `-D` で渡す。渡さない場合は `local`。`ping` の結果に含まれる。ファイルとしての版の情報は `build.json` のみ（[梱包](#梱包)） |
| mac | `Module-Info.plist.in`（版のキーは保持しない）・`.vwlibrary` バンドル・リソースは `Contents/Resources/<name>.vwr`。arm64 のみ（SDK リファレンスの `plugin/` が実機で読み込まれている構成に合わせ、検証していない構成を配布しない。CLI は universal。Intel の mac（`macos-x64`）は、実機で検証する環境が無いので対応を見送る）。配備の下限は mac 14。ソースは Objective-C++ としてコンパイルする |
| Windows | `.vlb`・隣に `.vwr` |
| ターゲット | `add_vw_plugin(VwCli "cli" io.github.min-nano.cli)` の 1 つのみ（安定版と開発版を区別しない） |

`add_vw_plugin` は、準拠した構成にある殻と本体の分割・同梱スクリプト・版のファイル（plist の版のキーなど）を
有しません。版の情報が `build.json` とコンパイル時の定義の 2 つ以外に分散しないようにするためです。

### CMake の版

`cmake_minimum_required(VERSION 3.24)` としています。理由は `CMakeLists.txt` の先頭の注釈の通りで、
3.23 以下で構成すると最初の行で停止します。

最小版はポリシーの既定も決定します。3.21〜3.24 で導入されたポリシー（CMP0121〜CMP0139）の内、
`CMakeLists.txt` が使用する記法に関わるのは次のもののみで、いずれも構成の結果を変更しません。
他は `CMakeLists.txt` が使用しないコマンドとモジュールと記法（`list()`・`find_*()`・`while()`・
`cmake_dependent_option()`・project 形式の `try_compile()`・`if()` の `PATH_EQUAL`・`ExternalProject`・
`CheckIPOSupported`・`CPack`・`UseSWIG` など）か、他のコンパイラー（ARMClang・MCST LCC・Watcom）のものです。

| ポリシー | NEW の振る舞い | このリポジトリ |
| --- | --- | --- |
| CMP0124（3.21） | `foreach()` のループ変数をループの外に保持しない | ループ変数（`stamp`・`required`・`test`）をループの後で読み取らない |
| CMP0126（3.21） | `set(CACHE)` が同名の通常の変数を削除しない | キャッシュに書き込む `CMAKE_OSX_ARCHITECTURES`・`CMAKE_OSX_DEPLOYMENT_TARGET` は `if(NOT DEFINED ...)` の中で設定し、同名の通常の変数が存在しない |
| CMP0128（3.22） | `<LANG>_EXTENSIONS` の既定をコンパイラーの既定から取得し、不要な言語の版の指定を付与しない | `CXX_STANDARD 20`・`CXX_STANDARD_REQUIRED ON`・`CXX_EXTENSIONS OFF` を明示している。`CXX_EXTENSIONS OFF` は GCC・clang の既定（GNU の拡張あり）と異なるので、既定の版が C++20 のコンパイラー（GCC 16 など）でも `-std=c++20` が必要な指定として付与される（MSVC は既定が C++14 なので `/std:c++20` が付与される）。mac のプラグインの Objective-C++ も、`OBJCXX_EXTENSIONS` を定めていないので `CXX_EXTENSIONS` に従い、いずれの既定でも `-std=c++20` |
| CMP0131（3.24） | `LINK_LIBRARIES` で `$<LINK_ONLY:...>` を使用できる | `$<LINK_ONLY>` を記述しない |
| CMP0132（3.24） | 初回の構成で `CC`・`CXX` などの環境変数を設定しない | `execute_process()` などで、環境変数のコンパイラーを読み取る子プロセスを構成の中で実行しない |

ubuntu（CMake 3.28・clang 18）で、テストの構成（`-DVW_BUILD_PLUGIN=OFF`）の
`compile_commands.json` とリンクの指定が 3.20 と 3.24 の既定で同一であること、OBJCXX を有効にした
最小の構成で上記の版の指定が同一であることを確認しています。プラグインの構成（SDK が必要）は
CI の `build.yml` と実機で検証します。

### ローカル環境でのビルド

mac のローカル環境でプラグインをビルドする場合は、プリセット `vw2026`（Vectorworks の版）を使用します。
共通の設定は、コミット済みの `CMakePresets.json` の隠しプリセット `vw2026-base` にあります
（ビルド先 `build-vw2026/`・`RelWithDebInfo`・生成器 `Unix Makefiles`）。各自の mac に固有のもの
（SDK の場所と並列数）のみを、コミットしない `CMakeUserPresets.json`（`.gitignore` 済み）に
記述して継承します。

```json
{
  "version": 2,
  "configurePresets": [
    {
      "name": "vw2026",
      "inherits": "vw2026-base",
      "cacheVariables": { "VW_SDK_DIR": "/Users/<you>/vw-sdk/2026-NNA-eng-mac-SDK" }
    }
  ],
  "buildPresets": [
    { "name": "vw2026", "inherits": "vw2026-base", "configurePreset": "vw2026", "jobs": 4 }
  ]
}
```

```sh
cmake --preset vw2026 && cmake --build --preset vw2026 --target VwCli
```

- **mac のみの手順です。** Windows は Visual Studio の生成器（`-A x64`）で、`CMAKE_BUILD_TYPE` が
  効かないので、このプリセットは使用しません（`CMakeLists.txt` の先頭の注釈の通り `--config Release`）。
- `RelWithDebInfo` は、Vectorworks が異常終了した場合に lldb で箇所を追跡できるようにデバッグ情報を
  付与したものです。clang では `Release` と最適化の段階（`-O2` / `-O3`）とデバッグ情報が異なります。
  最適化の違いで発現の仕方が変わる不具合は再現しないことがあるので、その場合は
  `cmake --preset vw2026 -DCMAKE_BUILD_TYPE=Release` で構成し直してビルドします。同じ
  `build-vw2026/` のキャッシュを書き換えるので、元に戻す場合は `cmake --preset vw2026` のみで
  構成し直します（プリセットの `RelWithDebInfo` に戻ります）。
- `VW_SDK_DIR` はリポジトリの外の絶対パスで記述します（`$env{HOME}/...` でも差し支えありません。理由は
  `.worktreeinclude` の注釈）。`CMakeLists.txt` は環境変数 `VW_SDK_DIR` を読み取らないので、環境変数で
  渡す場合も `"VW_SDK_DIR": "$env{VW_SDK_DIR}"` と `cacheVariables` に記述します。
- `jobs` は計算機に合わせて設定します（目安はコア数）。
- プリセットはスキーマの `version: 2`（CMake 3.20 から読み取れる）ですが、`CMakeLists.txt` が CMake 3.24 以上を要求します（[CMake の版](#cmake-の版)）。
- **Vectorworks 2025 のプラグインはローカル環境ではビルドしません**（理由は[版「ビルド」](editions.md#ビルド)）。

## CI

| ワークフロー | 起動 | 内容 |
| --- | --- | --- |
| `go.yml` | push / PR・マージキュー | Go のテスト（mac・Windows）と、配布する 3 つのターゲットへのビルドの検証（出力はアップロードしない）。テストとビルドは 2026 と 2025（`-tags vw2025`。[版](editions.md)）の両方で行う。両方の OS・両方の版のカバレッジを Codecov へ送信する（フラグ `go`）。ubuntu で `spool.NestingDepth` のファジングを 2 つの対象で 30 秒ずつ実行する |
| `format.yml` | push / PR・マージキュー | `scripts/format.sh`（[整形](../format.md)） |
| `cpp.yml` | push / PR・マージキュー | 無 SDK の単体テストと通信規則のフィクスチャの照合を mac・Windows・ubuntu で実行する。mac と Windows でカバレッジを測定し、共通部（`src/core`）の分を Codecov へ送信する（フラグ `cpp`）。ubuntu の clang で `core/Json` のファジングを 120 秒実行する。サニタイザの付与の方法とカバレッジの測定方法は `cpp.yml` の注釈 |
| `lint.yml` | push / PR・マージキュー | `scripts/lint.sh`（[検査](../lint.md)） |
| `build.yml` | push（main）/ PR・マージキュー / 手動 | mac と Windows のビルド・梱包・リリースと、公開した資産の検証（下記） |
| `hooks.yml` | push / PR・マージキュー | `cli` のフック（`scripts/hooks/`）のテスト。mac で `tests/hooks/hooks_test.sh`（`sh`）、Windows で `tests/hooks/Hooks.Tests.ps1`（Pester。Windows PowerShell 5.1）を実行する（[インストール「テスト」](install-and-update.md#テスト)） |
| `cleanup-dev-release.yml` | PR が閉じられたとき | 当該 PR のプレリリース（`dev-<slug>-<PR 番号>-<短い sha>`）と下書きのリリースを、リリースとタグごと全て削除する。`build.yml` と同じ `concurrency` のグループで実行され、当該 PR の実行中のビルドを停止してから削除する（削除した後に公開されないため）。グループの構成と再オープン（`reopened`）の扱いは、両方のワークフローの注釈 |

`build.yml` のジョブ:

| ジョブ | 内容 |
| --- | --- |
| `meta` | ビルドする sha・刻印・公開先のタグ（`0.0.0-<短い sha>` / `dev-<slug>-<PR 番号>-<短い sha>` / 公開しない）を決定し、`build.json` を書き出す（mac と Windows の zip に同じものを格納する） |
| `build-mac` | `macos-15`・Xcode 16.2（SDK リファレンスの `plugin/` と同じ）。プラグインのビルドとアドホック署名、Go の arm64 と amd64 を `lipo` で universal に統合、zip。版の行列で実行する（ジョブの名前と版ごとの違いは[版「ビルド」](editions.md#ビルド)） |
| `build-windows` | `windows-latest`・MSVC。プラグインのビルド、Go の amd64、zip。版の行列で実行する（同上） |
| `release` | 公開する場合のみ。版ごとにマニフェスト（`vw2026.json`・`vw2025.json`。[レジストリ「マニフェスト」](../registry.md#マニフェストvw2026json)）を書き出し、**下書きのリリース**に zip・マニフェスト・初回スクリプトをアップロードし終えてから公開する（下書きは利用者の URL から参照できないので、資産が揃っていない間の状態を見せない）。同じタグのリリースが既にある場合の扱いは[リリースの構成](#リリースの構成) |
| `verify` | 公開した資産を利用者と同じ経路（既定のレジストリの `cli` の規則が指すマニフェスト → その `asset`）で、mac と Windows で版ごとに取得して、マニフェスト・zip の構成・`build.json` と CLI の版を検証する。mac では、公開した初回スクリプトで一時の `<Plug-Ins>` にインストールしてアンインストールする。`--latest` を付けて公開した場合は、`latest` の規則でも取得する |
| `verify-result` | `verify` の結果を 1 つのチェックに集約する（行列のジョブは、スキップされると必須のチェックにできないため） |
| `install-windows` | PR のプレリリースを公開した場合のみ。`windows-latest` で、公開した PR のビルドを実物の exe とレジストリで置換・アンインストールする（手順は[インストール「テスト」](install-and-update.md#テスト)の「実物の exe の入れ替え」）。版の行列のジョブ `install-windows-editions` の結果を、必須のチェック `install-windows` が集約する（`verify-result` と同じ理由） |

各ジョブの検証事項の詳細は `build.yml` の注釈にあります。

必須のチェックとなるワークフローには `merge_group` を付与します（付与しないとキューの中で
チェックが開始されず、マージが進行しないため）。又 `paths` で絞り込みません。必須のチェックはパスごとに
必須かどうかを区別できないため、絞り込むと対象外の変更のみの PR（文書のみなど）でチェックが報告されず、
マージできなくなります。

- **Windows のカバレッジのツールの版は固定しません。** `Microsoft.CodeCoverage.Console` は単独で
  配布されず、ランナーのイメージの Visual Studio に付属するもののみを使用できます。MSVC と同様に
  イメージに従い、使用した版はジョブのログ（`--version` の出力）で確認します。イメージの更新で
  Codecov の `cpp` の数値が、このリポジトリのコミット無しに変動することがあります。
- **Codecov は、1 つのコミットの結果が 4 つ揃ってから判定します**（`codecov.yml` の
  `after_n_builds`。理由は同じファイルの注釈）。カバレッジを送信するワークフローや OS を増減した場合は、
  この数を合わせます（多すぎると判定が出ず、マージキューが時間切れで除外するため）。
- **設けないもの**（必要と判明してから追加します）: SDK に依存する clang-tidy とそのキャッシュ、
  CI 上で調査するためのワークフロー、リリースを定期的に検証するワークフロー。
- **SDK の取得に Secrets は不要です。** 公開の URL
  （`https://release.vectorworks.net/latest/Vectorworks/2026-NNA-eng-{mac,win}-SDK.zip`）から
  `scripts/fetch-vw-sdk.sh` で取得します。
- **SDK のキャッシュのキーは、配布されている SDK の版から決定します**（`scripts/vw-sdk-cache-key.sh`。
  URL が `latest` で SDK が差し替わっても変化しないため。版を読み取れない場合の扱いは同じスクリプトの注釈）。
- リリースは `GITHUB_TOKEN`（`contents: write`）で行います。
- 自動レビューは設けません（設ける場合は Secrets に `CLAUDE_CODE_OAUTH_TOKEN` が必要です）。
- 署名はアドホック（`codesign --force --deep --sign -`）です。Developer ID と公証は行いません。

## 梱包

`build.yml` の mac ジョブで Go もビルドし、`lipo` で universal に統合します。

```
cli-macos-arm64.zip                     cli-windows-x64.zip
└─ （zip の直下）                       └─ （zip の直下）
   ├─ cli.vwlibrary/                       ├─ cli.vlb ・ cli.vwr
   ├─ build.json                           ├─ build.json
   ├─ bin/vw2026          （universal）    ├─ bin\vw2026.exe
   └─ hooks/（install.sh・uninstall.sh・    └─ hooks\（install.ps1・uninstall.ps1・
            lib.sh）                                 lib.ps1）
```

2025 の zip（`cli-vw2025-macos-arm64.zip`・`cli-vw2025-windows-x64.zip`）も同じ構成で、`bin/` の CLI が `vw2025`
（`vw2025.exe`）です（[版](editions.md)）。

zip の内容はそのまま `<Plug-Ins>/cli/` に配置され、`bin/` の CLI もそこから実行します。`hooks/` のフックは
PATH のみを設定します（[インストール「CLI とスプールの配置場所」](install-and-update.md#cli-とスプールの配置場所)）。zip の項目の区切りは
`/` とします（`\` を区切りに使用した zip を `install` は拒否します。[レジストリ「zip の構成」](../registry.md#zip-の構成)）。

`build.json`（`{"version","branch"}`）は CI が書き出します。`ping` の `version` / `branch` と同じ値で、
`bin/vw2026 version` と照合するためのものです。`vw2026 install` が新しいかどうかを比較するのは
`build.json` ではなく、タグです（[インストール「処理の流れ」](install-and-update.md#処理の流れ)）。

## リリースの構成

**タグはビルドごとに新たに付与し、再利用しません**（[レジストリ「名前・タグ・ターゲット」](../registry.md#名前タグターゲット)）。
`vw2026 install` は既定のレジストリの `cli` の規則（`latest` は `releases/latest/download/vw2026.json`、
それ以外は `releases/download/<tag>/vw2026.json`）でマニフェストを取得し、その `asset` を取得します。
初回スクリプトは同じ形式の URL で zip を取得します。**この URL の形式と資産名（`vw2026.json`・
`cli-<target>.zip`・`get-vw2026.{sh,ps1}`）は変更しません。**

- main: main への push ごとに `0.0.0-<短い sha>` のリリースを作成します。タイトルは `0.0.0 (<sha>)` です。
  `latest`（`cli@latest`）はこれを指します。`--latest` を付けるのは、ビルドしたコミットが公開する時点の
  `main` の先頭である場合のみです（理由は `build.yml` の注釈）。タグの形式は[名前と識別子](identifiers.md)にあります。
  `install` はタグの大小を比較しない（異なればインストールする）ので、版の付け方を変更しても `install` は変わりません。
- PR: 同じリポジトリの PR で、**変更後のコードでビルドしたもの**を push ごとに
  `dev-<slug>-<PR 番号>-<短い sha>`（slug は[名前と識別子](identifiers.md)）としてプレリリースします。
  タイトルは **`Dev: <branch> (<sha>)`**、`--prerelease`（`latest` を動かさない）です。内容は main のものと
  **同じ名前のプラグイン**で、インストールすると置換されます（`vw2026 install cli@<そのタグ>`）。元に戻す場合は
  `vw2026 install cli@latest`（main の最新）です。PR を閉じると、当該 PR のプレリリースは全て削除されます。
  fork の PR・マージキュー・main 以外の手動の実行は、ビルドするのみで公開しません。
- **既に同じタグの公開済みのリリースがある場合は、そのリリースのタグが指すコミットが今回ビルドした
  完全な sha と一致するかを確認します。** 一致すれば何もせず（同じ sha の再実行）、異なれば（短い sha が
  以前のコミットと重複した）`release` を失敗とします。暗黙にスキップすると `latest` が古いビルドを指したまま
  になるためです。重複した場合は、タグの sha の文字数を増やすことを人が決定します。
- 古い main のリリースは削除しません（既存のタグを `install` が後から取得することはありませんが、
  `cli@<そのタグ>` で前の版へ戻せるように残置します）。
- PR の版は**ブランチの head** で決定します。`pull_request` の `GITHUB_SHA` は main と合わせた
  マージ用のコミットで、ブランチのいずれのコミットとも異なるので使用しません。ビルドは head
  （`github.event.pull_request.head.sha`）をチェックアウトして行い、`VW_BUILD_VERSION`・`build.json` の
  `version`・タイトルの `<sha>` にはその短い sha を、`VW_BUILD_BRANCH`・`build.json` の `branch`・
  タイトルの `<branch>` には `github.head_ref` を載せます。`ping` の `version` を
  `git log` の sha と照合できるようにするためです。head をチェックアウトするのは同じリポジトリの PR
  のみです（fork の head は信頼できないコードのため）。
- CI は `pull_request` のマージ用のコミットで実行され、プレリリースは head でビルドするので、
  ブランチが main から遅れていると両者の内容は異なります。ブランチを main に追従させることは
  要求しません（並行して進める PR ごとに取り込みを強制しないため）。扱いは
  [実機での検証方法「検証事項」](verification.md#検証事項)にあります。
- 資産は、`vw2026.json`（マニフェスト）・`cli-macos-arm64.zip` / `cli-windows-x64.zip`・
  `get-vw2026.{sh,ps1}`（初回スクリプト。利用者は `releases/latest/download/` のものを使用する）と、2025 の
  同じもの（`vw2025.json`・`cli-vw2025-macos-arm64.zip` / `cli-vw2025-windows-x64.zip`・`get-vw2025.{sh,ps1}`。[版](editions.md)）です。
  初回スクリプトはビルドしたコミットの雛形 `scripts/get-vw.{sh,ps1}` の `@VW_TAG@` を当該リリースのタグに、`@VW_YEAR@` を
  版に置換してアップロードします（いずれのリリースから取得したスクリプトも、当該リリースの当該版の `cli` を
  インストール・アンインストールする。[インストール「初回スクリプト」](install-and-update.md#初回スクリプトget-vw2026sh--ps1)）。
  PowerShell のものは ASCII のみで記述し、BOM を除去してアップロードします（理由は `build.yml` の注釈）。本文に
  機械が読み取る情報は載せません（インストールに必要な情報はマニフェストが保持する）。CLI 単体の資産は設けません
  （zip の `bin/` にあり、初回スクリプトもそこから取り出す）。
