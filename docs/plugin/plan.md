# 実装の順序

**1 段 1 PR**で、各段の終わりに確かめ方を決めておきます。実機でしか確かめられない段
（殻・本体・スクリプト）は、**利用者が実機で「確認できた」と言うまでマージしません**
（CI が通ることも、道具が応えることも、実機の確認の代わりにはならない）。

| 段 | 中身 | 確かめ方 | 実機 |
| --- | --- | --- | --- |
| 1 | **SDK に依らない共通部**: `core/Json`・`core/Bridge`・`core/Serve`（偽の道具の表）・`protocol/fixtures/`・`test.yml`・`lint.yml`・`CLAUDE.md` の追記（PR の進め方・消すコードの規約） | CI（ASan・UBSan）。Go 側のテストも同じ見本を読む | 不要 |
| 2 | **骨格**: CMake・`BuildConfig`・`ModuleMain`・ABI・`PayloadHost`/`Session`/`HostHolder`・`payload/PayloadMain`・`Clock`・道具 `tools` / `ping`・リソース・`build.yml`（ビルドと開発版のリリースまで）・`ci-debug.yml` | CI。実機で zip を手で置き、`vw2026 --channel dev status` / `call ping` が応える。本体を手で差し替えて再起動なしに `version` が変わる | 要 |
| 3 | **読む道具**: `layers` / `classes` / `layer_objects` / `object_counts` | 実機で、元のプラグインの `vw_*` と同じ図面に対して同じ結果になる | 要 |
| 4 | **配布**: 梱包（`bin/vw2026`）・`vw-install` / `vw-uninstall`（PATH を含む）・安定版のリリース・`cleanup-dev-release.yml`・スクリプトのテスト | CI（スクリプトのテストで、PATH の歯止めを押さえる）。実機でインストール → 新しい端末で `vw2026 status` → アンインストールでリンクだけが消える | 要 |
| 5 | **自動アップデート**: メニュー「アップデートを確認」・道具 `update` / `restart` | CI（`UpdaterFlowTests`）。実機で本体だけ変わる更新（再起動なし・`payload_version` が変わる）と、殻が変わる更新（`restart_if_needed`）の両方 | 要 |
| 6 | **状態と停止**: メニュー「CLI ブリッジの状態…」・停止と再開（[未決 1](open-questions.md)） | 実機で、停止すると `vw2026 status` が `live:false` になり、再開で戻る | 要 |

その先（順序は未定）:

- **他のプラグインの機能をユニバーサル名で呼ぶ**（[SDK リファレンス #217](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/217) の結果しだい）。
- **図面に書く道具**（undo の作法・人の操作との混在の扱いを決めてから。[未決 3](open-questions.md)）。
- 構造設計支援プラグインの開発版ブリッジの移行（当面は並存）。

## 実機の確かめ方（MCP なし）

このプラグインの実機確認は `vw2026` で行います（MCP は別のプロジェクト）。ローカルの
Claude Code セッションから `vw2026` を呼び、結果の JSON を読みます。図面に何かが起きたかは
人に確かめてもらいます。開発版の確認には `--channel dev` を付けます。

元のプラグインの開発版ブリッジ（`min-nano_structureDev-mcp`）とはスプールが別なので、
同時に動かして結果を比べられます（段 3）。
