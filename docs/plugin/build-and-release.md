# ビルドとリリース

元:`CMakeLists.txt` と元:`.github/workflows/build.yml` を移し、IFC の部分を除きます。

## CMake

| 項目 | 内容 |
| --- | --- |
| プロジェクト | `project(VwCli CXX)`（Apple では OBJCXX も） |
| 選択肢 | `VW_BUILD_PLUGIN`（ON）・`VW_BUILD_TESTS`（OFF）・`VW_ENABLE_SANITIZERS`・`VW_ENABLE_COVERAGE`・`VW_ENABLE_PCH`（ON）・`VW_BUILD_CHANNEL`（`stable` / `dev` / `both`。既定 `both`） |
| SDK | `VW_SDK_DIR`（`SDKLib` を含む）。mac は `libVWSDK.a`＋VWMM・`BuildVWR`、Windows は x64 だけ（`VWSDK.lib`・`VWMM.lib`・`buildvwr.exe`） |
| 共通部 | `VwCliCore`（STATIC。`src/core/*`。SDK に依らない。プラグインとテストがリンク） |
| プラグイン | `add_library(<target> MODULE)`。`VW_PLUGIN_SOURCES`（`src/*.cpp`・`src/tools/*`） |
| 刻印 | `VW_BUILD_VERSION`（短い sha）・`VW_BUILD_BRANCH` |
| mac | `Module-Info.plist.in`（`VWBuildChannel` / `VWBuildBranch` / `VWBuildCommit`）・`.vwlibrary` バンドル・リソースは `Contents/Resources/<name>.vwr` |
| Windows | `.vlb`・隣に `<name>.commit` / `.branch` と `.vwr` |
| 系列 | `add_vw_plugin(VwCli "cli" io.github.min-nano.cli stable)` / `add_vw_plugin(VwCliDev "cli_dev" io.github.min-nano.cli-dev dev DEV)` |

`add_vw_plugin` は元の関数（元:`CMakeLists.txt` L565-756）から、本体のターゲット・殻の ID・
同梱スクリプトを除いて使います。

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
   ├─ build.json                           ├─ cli.commit ・ cli.branch
   ├─ bin/vw2026          （universal）    ├─ build.json
   ├─ vw-install.sh                        ├─ bin\vw2026.exe
   └─ vw-uninstall.sh                      ├─ vw-install.ps1
                                           └─ vw-uninstall.ps1
```

`build.json`（`{"plugin","channel","version","branch"}`）は CI が書きます。
`vw2026 update` が「何が入っているか」を判定するのに使います
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
  呼ぶ側のプログラムの開発やテストで CLI だけを取りたいときのためです（CLI はスプールが
  ローカルにあるときだけ働くので、プラグインの代わりにはならない）。
