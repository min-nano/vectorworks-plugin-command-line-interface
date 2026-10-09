# ビルドとリリース

元:`CMakeLists.txt` と元:`.github/workflows/build.yml` を移し、IFC の部分を除きます。

## CMake

| 項目 | 内容 |
| --- | --- |
| プロジェクト | `project(VwCli CXX)`（Apple では OBJCXX も） |
| 選択肢 | `VW_BUILD_PLUGIN`（ON）・`VW_BUILD_TESTS`（OFF）・`VW_ENABLE_SANITIZERS`・`VW_ENABLE_COVERAGE`・`VW_ENABLE_PCH`（ON） |
| SDK | `VW_SDK_DIR`（`SDKLib` を含む）。mac は `libVWSDK.a`＋VWMM・`BuildVWR`、Windows は x64 だけ（`VWSDK.lib`・`VWMM.lib`・`buildvwr.exe`） |
| 共通部 | `VwCliCore`（STATIC。`src/core/*`。SDK に依らない。プラグインとテストがリンク） |
| プラグイン | `add_library(<target> MODULE)`。`VW_PLUGIN_SOURCES`（`src/*.cpp`・`src/tools/*`） |
| 刻印 | `VW_BUILD_VERSION`（短い sha）・`VW_BUILD_BRANCH`。コンパイル時の定義だけで、印（`bridge.json`）に載る。ファイルとしての版の情報は `build.json` だけ（下記） |
| mac | `Module-Info.plist.in`（版の鍵は持たない）・`.vwlibrary` バンドル・リソースは `Contents/Resources/<name>.vwr` |
| Windows | `.vlb`・隣に `.vwr` |
| ターゲット | `add_vw_plugin(VwCli "cli" io.github.min-nano.cli)` の 1 つだけ（安定版と開発版を区別しない） |

`add_vw_plugin` は元の関数（元:`CMakeLists.txt` L565-756）から、本体のターゲット・殻の ID・
同梱スクリプト・系列（`VW_DEV_BUILD`）の切り替え・版のファイル（`.commit` / `.branch`・
plist の `VWBuildBranch` / `VWBuildCommit`）を除いて使います。版の情報が `build.json` と
コンパイル時の定義の 2 つに散らばらないようにするためです。

## CI

| ワークフロー | 起動 | 中身 |
| --- | --- | --- |
| `cli.yml`（既存） | push / PR（`cli/**`） | Go のテスト（3 OS）とビルド |
| `test.yml` | push / PR | 無 SDK の単体テスト（ASan・UBSan）・スクリプトのテスト（bash・pwsh）・作法の見本の照合 |
| `lint.yml` | push / PR | clang-format・clang-tidy（無 SDK）・actionlint・shellcheck・PSScriptAnalyzer |
| `build.yml` | push（main）/ PR / 手動 | mac と Windows のビルド・梱包・リリース |
| `cleanup-dev-release.yml` | PR が閉じたとき | `dev-<slug>` を消す |

- **移さないもの**（要ると分かってから足す）: SDK に依存する clang-tidy とそのキャッシュ
  （元:`scripts/clang-tidy-sdk.sh`・`tidy-cache-key.py`）、`ci-debug.yml`、
  `stable-release-healthcheck.yml`。

- **SDK の取得に Secrets は要りません。** 元:`scripts/fetch-vw-sdk.sh` は公開の URL
  （`https://release.vectorworks.net/latest/Vectorworks/2026-NNA-eng-{mac,win}-SDK.zip`）から
  取ります。キャッシュのキーは `vw-sdk-2026-NNA-{mac,win}-v3`。
- リリースは `GITHUB_TOKEN`（`contents: write`）で行います。
- 自動レビュー（元の `pr-review.yml`）は差し当たり移しません（移すなら Secrets に
  `CLAUDE_CODE_OAUTH_TOKEN` が要る）。
- 署名は元と同じくアドホック（`codesign --force --deep --sign -`）。Developer ID と公証はしません。

## 梱包

`build.yml` の mac ジョブで Go もビルドし、`lipo` で universal にします（Go は arm64 の
バイナリをリンク時にアドホック署名する。`lipo` は各アーキテクチャの署名を保つ）。

```
cli.vwlibrary.zip                       cli.vlb.zip
└─ （zip の直下）                       └─ （zip の直下）
   ├─ cli.vwlibrary/                       ├─ cli.vlb ・ cli.vwr
   ├─ build.json                           ├─ build.json
   ├─ bin/vw2026          （universal）    ├─ bin\vw2026.exe
   ├─ vw-install.sh                        ├─ vw-install.ps1
   └─ vw-uninstall.sh                      └─ vw-uninstall.ps1
```

`build.json`（`{"version","branch"}`）は CI が書きます。版の情報を持つファイルはこれだけで、
`vw2026 update` が zip の中のものと入っているものを比べます（[更新](install-and-update.md#流れ)）。

## リリースの形

`vw2026 update` はタグと資産名から URL を組み立てて資産を直接取るので、**タグと資産名を
変えません**。

- main: main への push で転がりタグ `stable` を作り直す。タイトル `Stable (<sha>)`・`--latest`。
- PR: 同じリポジトリの PR で、**変更後のコードでビルドしたもの**を `dev-<slug>`（slug は
  [名前と識別子](identifiers.md)）にプレリリースする。タイトル **`Dev: <branch> (<sha>)`**・
  `--prerelease`。中身は main のものと**同じ名前のプラグイン**で、入れると入れ替わる
  （`vw2026 update --tag dev-<slug>`）。戻すときは `vw2026 update`（main の最新）。
- 資産: `cli.vwlibrary.zip` / `cli.vlb.zip` / `vw-install.{sh,ps1}` / `vw-uninstall.{sh,ps1}`。
  本文に機械が読む情報は載せません（版は zip の中の `build.json` が持つ）。CLI 単体の資産は
  持ちません（zip の `bin/` にある）。
