# 実装の順序

**1 段 1 PR**で、各段の終わりに確かめ方を決めておきます。実機でしか確かめられない段
（プラグイン・スクリプト）は、**利用者が実機で「確認できた」と言うまでマージしません**
（CI が通ることも、道具が応えることも、実機の確認の代わりにはならない）。

| 段 | 中身 | 確かめ方 | 実機 |
| --- | --- | --- | --- |
| 1 | **SDK に依らない共通部**: `core/Json`・`core/Bridge`・`core/Serve`（偽の道具の表）・`protocol/fixtures/`・`test.yml`・`lint.yml`・`CLAUDE.md` の追記（PR の進め方・消すコードの規約） | CI（ASan・UBSan）。Go 側のテストも同じ見本を読む | 不要 |
| 2 | **骨格**: CMake・`BuildConfig`・`ModuleMain`・`Clock`・道具 `tools` / `ping` / `quit`・リソース・`build.yml`（ビルドと PR のプレリリースまで） | CI。実機で zip を手で置き、`vw2026 status` が `running:true` になり `call ping` が応える（**拡張機能なしで読み込まれることの確認を兼ねる**。[構成](architecture.md#拡張機能を登録しない)）。`call quit` で保存の確認が出て、取り消すと `call ping` がまた応え、保存すると `wait --down` が終わる | 要 |
| 3 | **読む道具**: `layers` / `classes` / `layer_objects` / `object_counts` | 実機で、元のプラグインの `vw_*` と同じ図面に対して同じ結果になる | 要 |
| 4 | **配布**: 梱包（`bin/vw2026`）・`vw-install` / `vw-uninstall`（PATH・動いているときに止める確かめを含む）・main のリリース・`cleanup-dev-release.yml`・スクリプトのテスト | CI（スクリプトのテストで、PATH の歯止めと、Vectorworks が動いているときに何も変えないことを押さえる）。動いている CLI の入れ替え（Windows）を押さえる。実機でインストール → 新しい端末で `vw2026 status` → アンインストールでリンクだけが消える | 要 |
| 5 | **更新**: `vw2026 update`（資産の取得・新しいビルドの判定・インストーラの呼び出しと結果の読み替え） | CI（Go の単体テスト）。実機で、動いている間は終了コード 7 で何も変わらず、`call quit` → `wait --down` → `update` → `launch` → `wait` で印の `version` が変わる。`--tag dev-<slug>` で PR のプレリリースに入れ替わり、`update` で main に戻る | 要 |

その先（順序は未定）:

- **他のプラグインの機能をユニバーサル名で呼ぶ**（[SDK リファレンス #217](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/217) の結果しだい）。
- **図面に書く道具**（undo の作法・人の操作との混在の扱いを決めてから。[未決 1](open-questions.md)）。
- 構造設計支援プラグインの開発版ブリッジの移行（当面は並存）。

## 元のプラグインから移すもの

パスは元のリポジトリ（HEAD 6ffd221）のものです。**移したら名前空間を `VwCli` に、プラグイン名を
`cli` に置き換えます。** 経緯の説明は写さず、決めごとだけを持ってきます（経緯は
元の `docs/` を指す）。

### そのまま移す（名前の置き換えだけ）

| 元 | 先 | 備考 |
| --- | --- | --- |
| `src/PluginPrefix.h` | 同じ | PCH |
| `src/core/Json.{h,cpp}` | 同じ | |
| `src/core/Bridge.{h,cpp}` | 同じ | `bridgeSpoolDir` を `<CLI>/spool` を求める形に（環境変数を読まない）。ロック（`lock`）を足す。持ち主と権限の確かめ・印の定期的な書き直しを除く。受け付けの手順は `core/Serve` へ（[ブリッジ](bridge.md)） |
| `src/Module-Info.plist.in` | 同じ | 版の鍵（`VWBuildBranch` / `VWBuildCommit`）を除く |
| `scripts/vw-install.*` / `vw-uninstall.*` | 同じ | プラグイン名。PATH の扱い・Vectorworks が動いているときに止める確かめを足す。`--folder-only` は持たない（[インストール](install-and-update.md)） |
| `scripts/fetch-vw-sdk.sh` / `ci-common.sh` / `ci-wait.sh` / `lint.sh` | 同じ | `VW_REPO` |
| `tests/TestFramework.h` と `CoreJsonTests` / `CoreBridgeTests` / インストーラとアンインストーラのテスト | 同じ | インストーラのテストから、殻の ID が同じときの手順を除く |
| `.clang-format` / `.clang-tidy` / `.cmake-format.yaml` / `.editorconfig` / `PSScriptAnalyzerSettings.psd1` | 同じ | 先頭の注釈 |
| `.github/workflows/build.yml` / `test.yml` / `lint.yml` / `cleanup-dev-release.yml` | 同じ | 名前・資産名・`bin/` の梱包。SDK に依存する clang-tidy を除く（[ビルド](build-and-release.md)） |

### 作り替えて移す

| 元 | 先 | 作り替え |
| --- | --- | --- |
| `src/Extensions/ExtMcpPalette.cpp` のタイマー部（`StartMcpBridgeClock` / `ClockTick` / `ServeOnce` / `RunShellAction`） | `src/Clock.{h,cpp}` | パレットを除く。本体の読み込みを除き、`core::serve` を直接呼ぶ。終了の依頼は `quit` だけで、`serve` から戻ってから行う（`SettleReport` の受け渡しは要らない）。**常に**開始する（開発版の区別は無い） |
| `src/draw/McpBridge.cpp` | `src/core/Serve.{h,cpp}`＋`src/tools/*` | 手順（SDK に依らない）と道具（SDK 依存）を分ける。道具の名前から `vw_` を外す。印の形を作法に合わせる |
| `src/ModuleMain.cpp` | 同じ | 拡張機能を登録しない（[構成](architecture.md#拡張機能を登録しない)）。`StartClock()` を `#ifdef` なしで呼ぶ |
| `src/BuildConfig.h` | 同じ | [識別子](identifiers.md) |
| `resources/min-nano_structure.vwr` | `resources/cli.vwr` | `.vwstrings` は UTF-16LE（BOM 付き）・CRLF のまま。拡張機能を登録しないので鍵は最小限 |
| `CMakeLists.txt` | 同じ | IFC の部分・本体のターゲット・殻の ID（`VW_SHELL_INPUTS`）を除く。`VwCliCore` は `src/core/*` だけ |

### 移さないもの

上の表に無いものは移しません。元のアップデータ（`src/Updater*`）も移しません
（`vw2026 update` は API を使わず、決まった URL から資産を取る。[更新](install-and-update.md#更新vw2026-update)）。

## 実機の確かめ方

このプラグインの実機確認は `vw2026` で行います。ローカルの
Claude Code セッションから `vw2026` を呼び、結果の JSON を読みます。図面に何かが起きたかは
人に確かめてもらいます。PR の確認は、その PR のプレリリースを入れて行います（同じ名前で入れ替わる）。

元のプラグインの開発版ブリッジ（`min-nano_structureDev-mcp`）とはスプールが別なので、
同時に動かして結果を比べられます（段 3）。
