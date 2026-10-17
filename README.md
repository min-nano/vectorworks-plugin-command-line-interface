# vectorworks-plugin-command-line-interface

[![codecov](https://codecov.io/gh/min-nano/vectorworks-plugin-command-line-interface/graph/badge.svg)](https://codecov.io/gh/min-nano/vectorworks-plugin-command-line-interface)

**Vectorworks をコマンドラインやほかのプログラムから操作するための、Vectorworks 2026 用
プラグインと CLI です。**

- プラグイン（C++・VW SDK。`cli`）が Vectorworks の中でブリッジを常駐させる
- 同梱のコマンド `vw2026`（インストール先の Vectorworks の版の名前）や、通信規則を守る任意のプログラムが、ローカルのスプール越しに道具を呼ぶ

```sh
vw2026 status
vw2026 call tools
vw2026 call layers '{"include_sheets":false}'
```

> **開発中です。** いまあるのは通信規則と CLI（`cli/`）とプラグインの設計、プラグインの
> SDK に依らない共通部（`src/core`）と骨格（道具は `ping` だけ）・配布（`build.yml`）です。
> `vw2026 install` と読む道具はこれから作ります（[実装の順序](docs/plugin/plan.md)）。

## ドキュメント

| ファイル | 中身 |
| --- | --- |
| [docs/design.md](docs/design.md) | 目的・決めたこと・構成・安全の前提・配布・進め方 |
| [docs/plugin/](docs/plugin/README.md) | プラグインの設計（構成・ブリッジ・道具・識別子・ビルド・配布・根拠・実装の順序・未決） |
| [docs/protocol.md](docs/protocol.md) | 通信規則（プラグインと呼ぶ側の約束。真実はここ） |
| [docs/fixtures.md](docs/fixtures.md) | 通信規則の見本（`protocol/fixtures`。C++ と Go のテストが同じものを読む） |
| `vw2026 --help` | `vw2026` のコマンド・指定・終了コード（定義は [cli/cmd/vw2026/](cli/cmd/vw2026/)。`vw2026 help <command>` で各コマンド） |
| [docs/format.md](docs/format.md) | 整形の決まりとツール（`scripts/format.sh`） |
| [docs/lint.md](docs/lint.md) | 検査のツール（`scripts/lint.sh`） |
| [docs/sdk-research.md](docs/sdk-research.md) | SDK リファレンスでの調査を待っているもの |
| [docs/design-review.md](docs/design-review.md) | 設計のレビューの指摘（項目ごとに検討中） |
