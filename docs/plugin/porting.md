# 元のプラグインから移すもの

パスは元のリポジトリ（HEAD 6ffd221）のものです。**移したら名前空間を `VwCli` に、プラグイン名を
`cli` / `cli_dev` に置き換えます。** 経緯の説明は写さず、決めごとだけを持ってきます（経緯は
元の `docs/` を指す）。

## そのまま移す（名前の置き換えだけ）

| 元 | 先 | 備考 |
| --- | --- | --- |
| `src/PluginPrefix.h` | 同じ | PCH |
| `src/core/Json.{h,cpp}` | 同じ | |
| `src/core/Bridge.{h,cpp}` | 同じ | `bridgeSpoolDir` を `PLUGIN_SPOOL` を使う形に。受け付けの手順は `core/Serve` へ（[ブリッジ](bridge.md)） |
| `src/Module-Info.plist.in` | 同じ | |
| `scripts/vw-install.*` / `vw-uninstall.*` | 同じ | プラグイン名。PATH の扱い・Vectorworks が動いているときに止める確かめを足す（[インストール](install-and-update.md)） |
| `scripts/fetch-vw-sdk.sh` / `clang-tidy-sdk.sh` / `tidy-cache-key.py` / `ci-common.sh` / `ci-wait.sh` / `ci-debug*.sh` / `lint.sh` | 同じ | `VW_REPO` |
| `tests/TestFramework.h` と `CoreJsonTests` / `CoreBridgeTests` / インストーラとアンインストーラのテスト | 同じ | インストーラのテストから、殻の ID が同じときの手順を除く |
| `.clang-format` / `.clang-tidy` / `.cmake-format.yaml` / `.editorconfig` / `PSScriptAnalyzerSettings.psd1` | 同じ | 先頭の注釈 |
| `.github/workflows/build.yml` / `test.yml` / `lint.yml` / `ci-debug.yml` / `cleanup-dev-release.yml` / `stable-release-healthcheck.yml` | 同じ | 名前・資産名・`bin/` の梱包（[ビルド](build-and-release.md)） |

## 作り替えて移す

| 元 | 先 | 作り替え |
| --- | --- | --- |
| `src/Extensions/ExtMcpPalette.cpp` のタイマー部（`StartMcpBridgeClock` / `ClockTick` / `ServeOnce` / `RunShellAction`） | `src/Clock.{h,cpp}` | パレットを除く。本体の読み込みを除き、`core::serve` を直接呼ぶ。終了の依頼は `quit` だけで、`serve` から戻ってから行う（`SettleReport` の受け渡しは要らない）。**開発版だけでなく両方で**開始する |
| `src/draw/McpBridge.cpp` | `src/core/Serve.{h,cpp}`＋`src/tools/*` | 手順（SDK に依らない）と道具（SDK 依存）を分ける。道具の名前から `vw_` を外す。印の形を作法に合わせる |
| `src/ModuleMain.cpp` | 同じ | 拡張機能を登録しない（[構成](architecture.md#拡張機能を登録しない)）。`StartClock()` を `#ifdef` なしで呼ぶ |
| `src/BuildConfig.h` | 同じ | [識別子](identifiers.md) |
| `resources/min-nano_structure{,Dev}.vwr` | `resources/cli{,_dev}.vwr` | `.vwstrings` は UTF-16LE（BOM 付き）・CRLF のまま。拡張機能を登録しないので鍵は最小限 |
| `CMakeLists.txt` | 同じ | IFC の部分・本体のターゲット・殻の ID（`VW_SHELL_INPUTS`）を除く。`VwCliCore` は `src/core/*` だけ |

## 移さない

- IFC の解析・描画・PIO（`src/parse/` / `src/draw/` の描画 / `Extensions/ExtColumnMark` /
  `ExtShearWall` / `ExtMenu` / `ExtTestMenu`）と、そのテスト・フィクスチャ。
- 実機テスト（`draw/Feedback` / `core/FeedbackSession` / `core/FeedbackScratch`）。
- パレット（`resources/common.vwr/html/mcp.html`・`CExtMcpPalette`・`ExtMcpMenu`）。
- MCP サーバ（`scripts/mcp/`・`.mcp.json`）。このリポジトリには含めない。
- 自動レビュー（`pr-review.yml`）。差し当たり持たない。
- 殻と本体の分割（`src/PayloadAbi.h` / `PayloadHost.*` / `PayloadSession.*` / `PayloadHostHolder.h`・
  殻の ID・本体のターゲット）と、そのテスト（`PayloadPathTests` / `PayloadHostHolderTests`）。
  ホットリロードをしないので要らない（[構成](architecture.md#モジュールは-1-つ)）。
- アップデータ（`src/Updater*`・`Extensions/ExtMenuCheckUpdate`・`scripts/vw-update.*`・`vw-token.*`）と
  そのテスト（`UpdaterParseTests` / `UpdaterFlowTests` / `UpdaterRobustnessTests` / `vw-update.test.sh`）。
  更新は CLI が行う。リリースの探し方（`q-stable` / `q-dev` の問い合わせ・`dev-*` の並べ方・
  `branch=` の読み方）は Go で書き直すときの参考にする。
