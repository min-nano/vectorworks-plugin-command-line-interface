# vectorworks-plugin-command-line-interface

[![codecov](https://codecov.io/gh/min-nano/vectorworks-plugin-command-line-interface/graph/badge.svg)](https://codecov.io/gh/min-nano/vectorworks-plugin-command-line-interface)

**Vectorworks をコマンドラインから操作するコマンド `vw2026` と、そのための Vectorworks 用プラグインです。**
シェルスクリプトやほかのプログラム、AI エージェントから、動いている Vectorworks に次のことをさせられます。

- **図面を読む**: レイヤとクラスの一覧、レイヤの中の図形（種別・名前・クラス・外形）、種別ごとの図形の数
- **図面を扱う**: 開いている図面の一覧、新しいファイルへの保存、図面を閉じる
- **Vectorworks を扱う**: 起動、終了、動いているかの確認
- **プラグインを入れる**: このプラグイン自身と、対応するほかのプラグインのインストール・更新・アンインストール

結果は JSON で標準出力に返り、成否は終了コードで分かります。

| 対応する Vectorworks | OS | コマンド |
| --- | --- | --- |
| Vectorworks 2026 | macOS（Apple シリコン）・Windows（x64） | `vw2026` |
| Vectorworks 2025 | 同上 | `vw2025` |

## インストール

Vectorworks を終了してから、次を実行します。

macOS（ターミナル）:

```bash
curl -fsSL https://github.com/min-nano/vectorworks-plugin-command-line-interface/releases/latest/download/get-vw2026.sh | sh
```

Windows（PowerShell）:

```powershell
irm https://github.com/min-nano/vectorworks-plugin-command-line-interface/releases/latest/download/get-vw2026.ps1 | iex
```

1. **新しい端末を開く**と、`vw2026` が使えます。macOS で `~/.local/bin` が PATH に無いときは、表示される案内に
   従って PATH に足します。
2. **Vectorworks を起動します。** 起動の途中に「開発元不明のプラグイン」のダイアログが出て `cli` が「無効」と
   示されたら、`cli` を選んで「有効化」し、Vectorworks を起動し直します。
3. `vw2026 status` が `{"running":true,…}` を返せば使えます。

Vectorworks 2025 に入れるときは、URL の `get-vw2026` を `get-vw2025` に替えます（コマンドは `vw2025`）。

## 使い方

```bash
# Vectorworks を起動する
vw2026 launch
```

```bash
# 操作を受け付けているかを確かめる（起動を待つときは繰り返す）
vw2026 status
```

```bash
# プラグインの版と、図面が開いているかを見る
vw2026 call ping
```

```bash
# 使える道具の一覧を見る
vw2026 call tools
```

道具は `vw2026 call <道具> ['<引数の JSON>']` で呼びます。

```bash
vw2026 call layers
```

```bash
vw2026 call layer_objects '{"layer":"1F","limit":10}'
```

```bash
vw2026 call documents
```

`layers` の結果の例:

```json
{"layers":[{"name":"1F","kind":"design","referenced":false,"scale":50,"objects":13,"current":true}],"count":1}
```

### 図面を保存する・閉じる、Vectorworks を終了する

図面や Vectorworks に働く道具（`save`・`close`・`quit`）は、ほかのプログラムの操作と混ざらないように、
**セッションの中で**呼びます。

```bash
# セッションを始める。{"session":"<ID>"} が返る
vw2026 session start
```

```bash
# 以後の呼び出しにセッションを載せる（呼び出しごとに --session <ID> を付けてもよい）
export VW2026_SESSION=<ID>
```

```bash
# documents が返した path の図面を、新しいファイルへ保存する
vw2026 call save '{"document":"/Users/me/plan.vwx","to":"/Users/me/plan-2.vwx"}'
```

```bash
# 図面を閉じる
vw2026 call close '{"document":"/Users/me/plan-2.vwx"}'
```

```bash
# セッションを終える
vw2026 session end
```

- **`close` は保存の確認を出さずに閉じ、保存していない変更を捨てます。** 残したい変更は、先に `save` で
  新しいファイルへ保存します（`save` は既にあるファイルへは保存しません）。
- `quit`（`vw2026 call quit`）は、図面をすべて閉じてから呼びます。図面が開いていれば終了せず、開いている図面の
  一覧を返します。Vectorworks が終了すると、セッションも終わります。

コマンドの指定・出力・終了コードは `vw2026 --help` と `vw2026 help <コマンド>` に、道具ごとの引数と結果は
[docs/plugin/tools.md](docs/plugin/tools.md) にあります。

## 更新とアンインストール

更新は、Vectorworks を終了してから行います（Vectorworks が動いている間は何も変えません）。

```bash
vw2026 install cli@latest
```

アンインストールは、インストールに使ったスクリプトに `uninstall` を渡します。入っている版に合った
コマンドは `vw2026 uninstall cli` が表示します。

macOS:

```bash
curl -fsSL https://github.com/min-nano/vectorworks-plugin-command-line-interface/releases/latest/download/get-vw2026.sh | sh -s -- uninstall
```

Windows:

```powershell
& ([scriptblock]::Create((irm https://github.com/min-nano/vectorworks-plugin-command-line-interface/releases/latest/download/get-vw2026.ps1))) uninstall
```

## ドキュメント

開発者向けの文書です。

| ファイル | 中身 |
| --- | --- |
| [docs/design.md](docs/design.md) | 目的・決めたこと・構成・安全の前提・これから作るもの |
| [docs/plugin/](docs/plugin/README.md) | プラグインの設計（構成・ブリッジ・道具・識別子・ビルド・配布・実機の確かめ方・根拠・未決） |
| [docs/protocol.md](docs/protocol.md) | 通信規則（プラグインと呼ぶ側の約束。真実はここ） |
| [docs/fixtures.md](docs/fixtures.md) | 通信規則の見本（`protocol/fixtures`。C++ と Go のテストが同じものを読む） |
| [docs/registry.md](docs/registry.md) | `vw2026 install` が読むレジストリとマニフェストの書き方・フック・プラグインの提供側に求めること |
| `vw2026 --help` | `vw2026` のコマンド・指定・終了コード（定義は [cli/cmd/vw2026/](cli/cmd/vw2026/)。`vw2026 help <command>` で各コマンド） |
| [docs/format.md](docs/format.md) | 整形の決まりとツール（`scripts/format.sh`） |
| [docs/lint.md](docs/lint.md) | 検査のツール（`scripts/lint.sh`） |
| [docs/sdk-research.md](docs/sdk-research.md) | SDK リファレンスでの調査の索引と、待っているもの |
| [docs/design-review.md](docs/design-review.md) | 設計のレビューの指摘のうち、まだ決まっていないもの |
