# 元のプラグインから移すもの

パスは元のリポジトリ（HEAD 6ffd221）のものです。**移したら名前空間を `VwCli` に、プラグイン名を
`cli` / `cliDev` に置き換えます。** 経緯の説明は写さず、決めごとだけを持ってきます（経緯は
元の `docs/` を指す）。

## そのまま移す（名前の置き換えだけ）

| 元 | 先 | 備考 |
| --- | --- | --- |
| `src/PluginPrefix.h` | 同じ | PCH |
| `src/PayloadHost.{h,cpp}` | 同じ | 解決する関数を 5 つに減らす（[ABI](abi.md)）。`runBundledScript` の実装を除く。L466 の「殻の ID を貸す」という古い注釈は写さない |
| `src/PayloadSession.{h,cpp}` | 同じ | `PayloadUse` / `ReleaseLoadedPayload` / `PayloadInUse` |
| `src/PayloadHostHolder.h` | 同じ | `canRunScripts` / `runScript` を除く |
| `src/core/Json.{h,cpp}` | 同じ | |
| `src/core/Bridge.{h,cpp}` | 同じ | `bridgeSpoolDir` を `PLUGIN_SPOOL` を使う形に。受け付けの手順は `core/Serve` へ（[ブリッジ](bridge.md)） |
| `src/Updater.{h,cpp}` / `UpdaterFlow.cpp` / `UpdaterHost.h` / `UpdaterParse.h` | 同じ | プラグイン名・表示名の定数（`UpdaterFlow.cpp` L32-34）。`RemoteDevUpdateWith` を安定版にも使えるよう一般化 |
| `src/Module-Info.plist.in` | 同じ | |
| `scripts/vw-install.*` / `vw-uninstall.*` / `vw-update.*` / `vw-token.*` | 同じ | `VW_REPO`・プラグイン名・トークンの名前。PATH の扱いを足す（[インストール](install-and-update.md)） |
| `scripts/fetch-vw-sdk.sh` / `clang-tidy-sdk.sh` / `tidy-cache-key.py` / `ci-common.sh` / `ci-wait.sh` / `ci-debug*.sh` / `lint.sh` | 同じ | `VW_REPO` |
| `tests/TestFramework.h` と `PayloadPathTests` / `PayloadHostHolderTests` / `CoreJsonTests` / `CoreBridgeTests` / `UpdaterParseTests` / `UpdaterFlowTests` / `UpdaterRobustnessTests` / スクリプトのテスト | 同じ | |
| `.clang-format` / `.clang-tidy` / `.cmake-format.yaml` / `.editorconfig` / `PSScriptAnalyzerSettings.psd1` | 同じ | 先頭の注釈 |
| `.github/workflows/build.yml` / `test.yml` / `lint.yml` / `ci-debug.yml` / `cleanup-dev-release.yml` / `stable-release-healthcheck.yml` | 同じ | 名前・資産名・`bin/` の梱包（[ビルド](build-and-release.md)） |

## 作り替えて移す

| 元 | 先 | 作り替え |
| --- | --- | --- |
| `src/Extensions/ExtMcpPalette.cpp` のタイマー部（`StartMcpBridgeClock` / `ClockTick` / `ServeOnce` / `RunShellAction` / `SettleReport`） | `src/Clock.{h,cpp}` | パレットを除く。道具の名前を `update` / `restart` に。**開発版だけでなく両方で**開始する。停止中は何もしない |
| `src/draw/McpBridge.cpp` | `src/core/Serve.{h,cpp}`＋`src/tools/*` | 手順（SDK に依らない）と道具（SDK 依存）を分ける。道具の名前から `vw_` を外す。印の形を作法に合わせる |
| `src/ModuleMain.cpp` | 同じ | 登録はメニュー 2 つだけ。`StartClock()` を `#ifdef` なしで呼ぶ |
| `src/BuildConfig.h` | 同じ | [識別子](identifiers.md) |
| `src/Extensions/ExtMenuCheckUpdate.*` | 同じ | UUID・ユニバーサル名 |
| `resources/min-nano_structure{,Dev}.vwr` | `resources/cli{,Dev}.vwr` | `.vwstrings` は UTF-16LE（BOM 付き）・CRLF のまま。鍵は `category` / `updateTitle` / `updateHelp` / `statusTitle` / `statusHelp` |
| `CMakeLists.txt` | 同じ | IFC の部分を除く。`VwCliCore` は `src/core/*` だけ |

## 移さない

- IFC の解析・描画・PIO（`src/parse/` / `src/draw/` の描画 / `Extensions/ExtColumnMark` /
  `ExtShearWall` / `ExtMenu` / `ExtTestMenu`）と、そのテスト・フィクスチャ。
- 実機テスト（`draw/Feedback` / `core/FeedbackSession` / `core/FeedbackScratch`）。
- パレット（`resources/common.vwr/html/mcp.html`・`CExtMcpPalette`・`ExtMcpMenu`）。
- MCP サーバ（`scripts/mcp/`・`.mcp.json`）。このリポジトリには含めない。
- 自動レビュー（`pr-review.yml`）は[未決 5](open-questions.md)。
