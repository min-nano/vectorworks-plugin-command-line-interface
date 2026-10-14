# ビルドとリリース

1 から書きます。SDK のリンクとバンドルの形は、SDK リファレンスの実機確認プラグイン（[`plugin/`](https://github.com/min-nano/vectorworks-developer-sdk-reference/tree/main/plugin)）の `CMakeLists.txt` で実機に読み込まれている形を見本にします（[プラグインの設計「根拠」](README.md#根拠)）。

## CMake

| 項目 | 内容 |
| --- | --- |
| プロジェクト | `project(VwCli CXX)`（Apple では OBJCXX も） |
| 選択肢 | `VW_BUILD_PLUGIN`（ON）・`VW_BUILD_TESTS`（OFF）・`VW_BUILD_FUZZERS`（OFF。clang の libFuzzer が要る）・`VW_ENABLE_SANITIZERS`（OFF。ASan と UBSan。MSVC は ASan だけ）・`VW_ENABLE_COVERAGE`（OFF。clang は source-based coverage、MSVC は `/PROFILE` を付けてリンクし Visual Studio の `Microsoft.CodeCoverage.Console` で測る。clang-cl は不可）・`VW_ENABLE_PCH`（ON）。段 1 にあるのは `VW_BUILD_TESTS`・`VW_BUILD_FUZZERS`・`VW_ENABLE_SANITIZERS`・`VW_ENABLE_COVERAGE` だけで、ほかはプラグインを足す段 2 から |
| SDK | `VW_SDK_DIR`（`SDKLib` を含む）。mac は `libVWSDK.a`＋VWMM・`BuildVWR`、Windows は x64 だけ（`VWSDK.lib`・`VWMM.lib`・`buildvwr.exe`） |
| 共通部 | `VwCliCore`（STATIC。`src/core/*`。SDK に依らない。プラグインとテストがリンク。OS ごとの呼び出しは `FilesPosix.cpp` / `FilesWindows.cpp` の片方） |
| テスト | `VwCliTests`（`tests/*Tests.cpp`。外部のテストの枠組みに依らない）・`JsonFuzzer`（`tests/JsonFuzzer.cpp`）。テストは `protocol/fixtures` を読む |
| プラグイン | `add_library(<target> MODULE)`。`VW_PLUGIN_SOURCES`（`src/*.cpp`・`src/tools/*`） |
| 刻印 | `VW_BUILD_VERSION`（短い sha）・`VW_BUILD_BRANCH`。コンパイル時の定義だけで、`ping` の結果に載る。ファイルとしての版の情報は `build.json` だけ（下記） |
| mac | `Module-Info.plist.in`（版の鍵は持たない）・`.vwlibrary` バンドル・リソースは `Contents/Resources/<name>.vwr` |
| Windows | `.vlb`・隣に `.vwr` |
| ターゲット | `add_vw_plugin(VwCli "cli" io.github.min-nano.cli)` の 1 つだけ（安定版と開発版を区別しない） |

`add_vw_plugin` は、見本にある殻と本体の分割・同梱スクリプト・版のファイル（plist の版の鍵など）を
持ちません。版の情報が `build.json` とコンパイル時の定義の 2 つ以外に散らばらないようにするためです。

## CI

| ワークフロー | 起動 | 中身 |
| --- | --- | --- |
| `go.yml`（既存） | push / PR・マージキュー | Go のテスト（macOS・Windows）とビルド。両方の OS のカバレッジを Codecov へ送る（どちらもフラグ `go`）。ubuntu で `spool.NestingDepth` のファジングを 2 つの対象で 30 秒ずつ回す |
| `format.yml`（既存） | push / PR・マージキュー | `scripts/format.sh`（[整形](../format.md)。gofmt・shfmt・clang-format・PSScriptAnalyzer） |
| `cpp.yml` | push / PR・マージキュー | 無 SDK の単体テストと通信規則の見本の照合（macOS は ASan・UBSan 付き、Windows は MSVC で付けない）。macOS（clang の source-based coverage）と Windows（MSVC のビルドを `Microsoft.CodeCoverage.Console` で）の単体テストでカバレッジを測り、共通部（`src/core`）の分を Codecov へ送る（どちらもフラグ `cpp`。`FilesWindows.cpp` は Windows の分にだけ入る。Windows の範囲は `tests/MsvcCoverage.xml`。Windows はテストを計測の下で 1 回だけ走らせる）。ubuntu の clang で同じテストを ASan・UBSan 付きで走らせ、`core/Json` のファジングを 120 秒回す |
| `lint.yml`（既存） | push / PR・マージキュー | `scripts/lint.sh`（[検査](../lint.md)。go vet・actionlint・shellcheck・clang-tidy（無 SDK）・PSScriptAnalyzer） |
| `build.yml` | push（main）/ PR / 手動 | mac と Windows のビルド・梱包・リリース |
| `cleanup-dev-release.yml` | PR が閉じたとき | `dev-<slug>` を消す |

必須のチェックになるワークフローには `merge_group` を付けます（付けないとキューの中で
チェックが始まらず、マージが進まない）。また `paths` で絞りません。必須のチェックはパスごとに
必須かどうかを分けられないため、絞ると対象外の変更だけの PR（文書だけ等）でチェックが報告されず、
マージできなくなります。

- **Windows のカバレッジの道具の版は固定しない。** `Microsoft.CodeCoverage.Console` は単独で
  配られず、ランナーのイメージの Visual Studio に付属するものだけを使える。MSVC と同じく
  イメージに従い、使った版はジョブのログ（`--version` の出力）で確かめる。イメージの更新で
  Codecov の `cpp` の数値が、このリポジトリのコミット無しに動くことがある。
- **持たないもの**（要ると分かってから足す）: SDK に依存する clang-tidy とそのキャッシュ、
  CI の上で調べるためのワークフロー、リリースの定期的な確認。

- **SDK の取得に Secrets は要りません。** SDK リファレンスの [`scripts/fetch-vw-sdk.sh`](https://github.com/min-nano/vectorworks-developer-sdk-reference/tree/main/scripts) と同じく、公開の URL
  （`https://release.vectorworks.net/latest/Vectorworks/2026-NNA-eng-{mac,win}-SDK.zip`）から
  取ります。
- **SDK のキャッシュのキーは、配布されている SDK の版から決めます**（SDK リファレンスの
  `scripts/vw-sdk-cache-key.sh` と同じく、HEAD の `ETag` か `Last-Modified` を混ぜる）。URL は
  `latest` で、SDK が差し替わっても同じなので、固定のキーでは古い SDK でビルドし続けます。
- リリースは `GITHUB_TOKEN`（`contents: write`）で行います。
- 自動レビューは差し当たり持ちません（持つなら Secrets に `CLAUDE_CODE_OAUTH_TOKEN` が要る）。
- 署名はアドホック（`codesign --force --deep --sign -`）。Developer ID と公証はしません。

## 梱包

`build.yml` の mac ジョブで Go もビルドし、`lipo` で universal にします（Go は arm64 の
バイナリをリンク時にアドホック署名する。`lipo` は各アーキテクチャの署名を保つ）。

```
cli.vwlibrary.zip                       cli.vlb.zip
└─ （zip の直下）                       └─ （zip の直下）
   ├─ cli.vwlibrary/                       ├─ cli.vlb ・ cli.vwr
   ├─ build.json                           ├─ build.json
   └─ bin/vw2026          （universal）    └─ bin\vw2026.exe
```

`build.json`（`{"version","branch"}`）は CI が書きます。版の情報を持つファイルはこれだけで、
`vw2026 install` が zip の中のものと入っているものを比べます（[インストール](install-and-update.md#流れ)）。

## リリースの形

`vw2026 install` と初回のスクリプトはタグと資産名から URL を組み立てて資産を直接
取るので、**タグと資産名を変えません**。

- main: main への push で転がりタグ `stable` を作り直す。タイトル `Stable (<sha>)`・`--latest`。
- PR: 同じリポジトリの PR で、**変更後のコードでビルドしたもの**を `dev-<slug>`（slug は
  [名前と識別子](identifiers.md)）にプレリリースする。タイトル **`Dev: <branch> (<sha>)`**・
  `--prerelease`。中身は main のものと**同じ名前のプラグイン**で、入れると入れ替わる
  （`vw2026 install --tag dev-<slug>`）。戻すときは `vw2026 install`（main の最新）。
- 資産: `cli.vwlibrary.zip` / `cli.vlb.zip` / `get-vw2026.{sh,ps1}`（初回のスクリプト。
  利用者は `stable` のものを使う）。本文に機械が読む情報は載せません（版は zip の中の
  `build.json` が持つ）。CLI 単体の資産は持ちません（zip の `bin/` にあり、初回の
  スクリプトもそこから取り出す）。
