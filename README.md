# vectorworks-plugin-command-line-interface

**Vectorworks をコマンドラインやほかのプログラムから操作するための、Vectorworks 2026 用
プラグインと CLI です。**

- プラグイン（C++・VW SDK。`cli`）が Vectorworks の中でブリッジを常駐させる
- 同梱のコマンド `vw2026`（インストール先の Vectorworks の版の名前）や、作法を守る任意のプログラムが、ローカルのスプール越しに道具を呼ぶ

```sh
vw2026 status
vw2026 tools
vw2026 call layers '{"include_sheets":false}'
```

> **開発中です。** いまあるのは受け渡しの作法と CLI（`cli/`）とプラグインの設計だけで、
> プラグインはこれから作ります（[実装の順序](docs/plugin/plan.md)）。

## ドキュメント

| ファイル | 中身 |
| --- | --- |
| [docs/design.md](docs/design.md) | 目的・決めたこと・構成・安全の前提・配布・進め方 |
| [docs/plugin/](docs/plugin/README.md) | プラグインの設計（構成・ブリッジ・道具・識別子・ビルド・配布・移植・実装の順序・未決） |
| [docs/protocol.md](docs/protocol.md) | 受け渡しの作法（プラグインと呼ぶ側の約束。真実はここ） |
| [docs/cli.md](docs/cli.md) | `vw2026` のコマンド・指定・終了コード・ビルド |
| [docs/sdk-research.md](docs/sdk-research.md) | SDK リファレンスでの調査を待っているもの |
