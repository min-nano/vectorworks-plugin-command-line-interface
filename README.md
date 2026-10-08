# vectorworks-plugin-command-line-interface

**Vectorworks をコマンドラインやほかのプログラムから操作するための、Vectorworks 2026 用
プラグインと CLI です。**

- プラグイン（C++・VW SDK。仮称 `min-nano_cli`）が Vectorworks の中でブリッジを常駐させる
- 同梱の `vwcli` や、作法を守る任意のプログラムが、ローカルのスプール越しに道具を呼ぶ

```sh
vwcli status
vwcli tools
vwcli call layers '{"include_sheets":false}'
```

> **開発中です。** いまあるのは受け渡しの作法と CLI（`cli/`）だけで、プラグインはこれから
> 作ります（[設計「進め方」](docs/design.md#進め方)）。

## ドキュメント

| ファイル | 中身 |
| --- | --- |
| [docs/design.md](docs/design.md) | 目的・決めたこと・構成・安全の前提・配布・進め方 |
| [docs/protocol.md](docs/protocol.md) | 受け渡しの作法（プラグインと呼ぶ側の約束。真実はここ） |
| [docs/cli.md](docs/cli.md) | `vwcli` のコマンド・指定・終了コード・ビルド |
| [docs/sdk-research.md](docs/sdk-research.md) | SDK リファレンスでの調査を待っているもの |

MCP（Claude などから安全に使うためのラッパー）は別のプロジェクトです。
