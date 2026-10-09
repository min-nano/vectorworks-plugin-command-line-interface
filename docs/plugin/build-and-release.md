# ビルドとリリース

元:`CMakeLists.txt` と元:`.github/workflows/build.yml` を移し、IFC の部分を除きます。

## CMake

| 項目 | 内容 |
| --- | --- |
| プロジェクト | `project(VwCli CXX)`（Apple では OBJCXX も） |
| 選択肢 | `VW_BUILD_PLUGIN`（ON）・`VW_BUILD_TESTS`（OFF）・`VW_ENABLE_SANITIZERS`・`VW_ENABLE_COVERAGE`・`VW_ENABLE_PCH`（ON）・`VW_BUILD_CHANNEL`（`stable` / `dev` / `both`。既定 `both`） |
| SDK | `VW_SDK_DIR`（`SDKLib` を含む）。mac は `libVWSDK.a`＋VWMM・`BuildVWR`、Windows は x64 だけ（`VWSDK.lib`・`VWMM.lib`・`buildvwr.exe`） |
| 共通部 | `VwCliCore`（STATIC。`src/core/*`。SDK に依らない。本体とテストだけがリンク） |
| 殻 | `add_library(<target> MODULE)`。`VW_SHELL_SOURCES` |
| 本体 | `<target>Payload`（MODULE・`PREFIX ""`・`SUFFIX ".vwpayload"`・`BUNDLE FALSE`・`ARCHIVE_OUTPUT_NAME <name>Payload`）。`VW_PAYLOAD_SOURCES` |
| 刻印 | `VW_BUILD_VERSION`（短い sha）・`VW_BUILD_BRANCH`・`VW_SHELL_ID` を両方のモジュールへ |
| mac | `Module-Info.plist.in`（`VWBuildChannel` / `VWBuildBranch` / `VWBuildCommit` / `VWShellId`）・`.vwlibrary` バンドル・リソースは `Contents/Resources/<name>.vwr`・同梱スクリプトは `Contents/Resources/` |
| Windows | `.vlb`・隣に `<name>.commit` / `.branch` / `.shell-id` と `.vwr`・同梱スクリプト |
| 系列 | `add_vw_plugin(VwCli "cli" io.github.min-nano.cli stable)` / `add_vw_plugin(VwCliDev "cli_dev" io.github.min-nano.cli-dev dev DEV)` |

`add_vw_plugin` は元の関数（元:`CMakeLists.txt` L565-756）をそのまま使えます。

### 殻の ID

`VW_SHELL_INPUTS` には**殻にコンパイルされるものだけ**を並べます。

```
CMakeLists.txt  src/PluginPrefix.h  src/BuildConfig.h  src/Module-Info.plist.in
src/ModuleMain.cpp  src/PayloadAbi.h  src/PayloadHost.{h,cpp}  src/PayloadSession.{h,cpp}
src/Clock.{h,cpp}  resources/
```

`src/core/`・`src/tools/`・`src/payload/`・同梱スクリプト・`cli/` は**入れません**（入れると
そこを直すたびに再起動を強いる。元の M23 で実際に起きた）。`resources/`（`.vwr`）は殻の
側のファイルなので入れます。殻の ID が同じ更新ではインストーラが殻のファイルに触れない
（[更新「殻の ID が同じとき」](install-and-update.md#殻の-id-が同じときファイルの上書き)）ので、
入れないと `.vwr` の変更が入らないためです。改行を LF に揃えて SHA-256 を取り、
12 文字に切ったものが `VW_SHELL_ID` です。

## CI

| ワークフロー | 起動 | 中身 |
| --- | --- | --- |
| `cli.yml`（既存） | push / PR（`cli/**`） | Go のテスト（3 OS）とビルド |
| `test.yml` | push / PR | 無 SDK の単体テスト（ASan・UBSan）・スクリプトのテスト（bash・pwsh）・作法の見本の照合 |
| `lint.yml` | push / PR | clang-format・clang-tidy（無 SDK）・actionlint・shellcheck・PSScriptAnalyzer |
| `build.yml` | push（main）/ PR / 手動 | mac と Windows のビルド・clang-tidy（SDK 依存）・梱包・リリース |
| `cleanup-dev-release.yml` | PR が閉じたとき | `dev-<slug>` を消す |
| `ci-debug.yml` | 手動 | SDK の検索・コンパイルの確かめ（元の `ci-debug` を移す） |

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
   ├─ cli.vwpayload                        ├─ cli.commit ・ cli.branch ・ cli.shell-id
   ├─ build.json ・ shell-id               ├─ cli.vwpayload
   ├─ bin/vw2026          （universal）    ├─ build.json ・ shell-id
   ├─ vw-install.sh                        ├─ bin\vw2026.exe
   └─ vw-uninstall.sh                      ├─ vw-install.ps1
                                           └─ vw-uninstall.ps1
```

`build.json`（`{"plugin","channel","version","branch","shell_id"}`）と `shell-id`（殻の ID だけの
1 行。殻が JSON を読まずに済むように分けてある）は CI が書きます。
`vw2026 update` が「何が入っているか」と「殻が変わるか」を判定するのに使います
（[更新](install-and-update.md#流れ)）。

## リリースの形

元と同じ形に保ちます（`vw2026 update` が読む）。

- 安定版: main への push で転がりタグ `stable` を作り直す。タイトル `Stable (<sha>)`・`--latest`。
- 開発版: 同じリポジトリの PR で `dev-<slug>`（ブランチ名の `/:@ ` を `-` にし、ほかの文字を
  落とす）。タイトル **`Dev: <branch> (<sha>)`**・`--prerelease`。
- 本文に `channel=` / `branch=` / `commit=` / `built=` を載せる（`vw2026 update --branch` が `branch=` を読む）。
- 資産: `cli.vwlibrary.zip` / `cli.vlb.zip` / `vw-install.{sh,ps1}` / `vw-uninstall.{sh,ps1}`
  （開発版は `cli_dev.*`）。
- **CLI 単体の資産**（`vw2026-darwin-universal` / `vw2026-windows-amd64.exe`）も添えます。
  ラッパー（MCP など）の開発やテストで CLI だけを取りたいときのためです（CLI はスプールが
  ローカルにあるときだけ働くので、プラグインの代わりにはならない）。
