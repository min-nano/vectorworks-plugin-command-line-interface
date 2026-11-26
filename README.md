# vectorworks-plugin-command-line-interface

[![codecov](https://codecov.io/gh/min-nano/vectorworks-plugin-command-line-interface/graph/badge.svg)](https://codecov.io/gh/min-nano/vectorworks-plugin-command-line-interface)

**Vectorworks をコマンドラインから操作するコマンドと、Vectorworks がコマンドを受信するための Vectorworks 用プラグインです。**
操作できるのは、インストールしてライセンス認証を済ませた Vectorworks です。`vw2026 call` の実行は、Vectorworks を起動し、
プラグインの動作中のみ操作可能です。
シェルスクリプトや他のプログラム、AI エージェントから、次のことを実行できます。

- **図面を読む**: レイヤとクラスの一覧、レイヤの中の図形（種別・名前・クラス・外形）、種別ごとの図形の数
- **図面を扱う**: 開いている図面の一覧、新しいファイルへの保存、図面を閉じる
- **Vectorworks を扱う**: 起動、終了、動いているかの確認
- **プラグインのインストール**: 対応するプラグインのインストール・更新・アンインストール

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
# {"outcome":"installed","name":"cli","requested":"<タグ>","tag":"<タグ>",…}
```

Windows（PowerShell）:

```powershell
irm https://github.com/min-nano/vectorworks-plugin-command-line-interface/releases/latest/download/get-vw2026.ps1 | iex
# {"outcome":"installed","name":"cli","requested":"<タグ>","tag":"<タグ>",…}
```

1. **新しい端末を開く**と、`vw2026` が使えます。macOS で `~/.local/bin` が PATH に無いときは、表示される案内に
   従って PATH に足します。
2. **Vectorworks を起動します。** 起動の途中に「開発元不明のプラグイン」のダイアログが出て `cli` が「無効」と
   示されたら、`cli` を選んで「有効化」し、Vectorworks を起動し直します。
3. 新しい端末で、次の結果が返れば使えます。

   ```bash
   vw2026 status
   # {"running":true,"spool":"…"}
   ```

Vectorworks 2025 に入れるときは、URL の `get-vw2026` を `get-vw2025` に替えます（コマンドは `vw2025`）。

## 使い方

結果は JSON で標準出力に返ります。

```bash
# Vectorworks を起動する
vw2026 launch
# {"launched":true,"command":[…]}

# 操作を受け付けているかを確かめる（起動を待つときは、running が true になるまで繰り返す）
vw2026 status
# {"running":true,"spool":"…"}

# プラグインの版と、図面が開いているかを見る
vw2026 call ping
# {"version":"abc1234","branch":"main","protocol":…,"pid":4242,"session":false,"document_open":true,"current_layer":"1F"}

# 使える道具の一覧を見る
vw2026 call tools
# {"protocol":…,"tools":[{"name":"layers","description":"…","inputSchema":{…},"kind":"read"},…]}
```

道具は `vw2026 call <道具> ['<引数の JSON>']` で呼びます。

```bash
# 開いている図面の一覧を見る
vw2026 call documents
# {"documents":[{"name":"plan.vwx","path":"/Users/me/plan.vwx","untitled":false,"active":true}],"count":1}

# レイヤの一覧を見る
vw2026 call layers
# {"layers":[{"name":"1F","kind":"design","referenced":false,"scale":50,"objects":13,"current":true}],"count":1}

# レイヤ 1F の図形を 10 個まで見る
vw2026 call layer_objects '{"layer":"1F","limit":10}'
# {"layer":"1F","unit":"mm","objects":[{"index":0,"type":3,"type_name":"box","pio_name":null,"name":"","class":"…","bounds":{…}},…],"returned":10,"total":13}
```

### 図面を保存する・閉じる、Vectorworks を終了する

Vectorworks に指示する操作（`save`・`close`・`quit`）は、他のプログラムの操作と混ざらないように、
**セッションを使って占有した中でだけ**実行できます（占有していないとき・ほかが占有しているときは、標準出力に何も返さず終了コード 23 で断られます。理由は標準エラーに出ます）。

```bash
# セッションを始める
vw2026 session start
# {"session":"<ID>"}

# 以後の呼び出しにセッションを載せる（呼び出しごとに --session <ID> を付けてもよい）
export VW2026_SESSION=<ID>

# documents が返した path の図面を、新しいファイルへ保存する
vw2026 call save '{"document":"/Users/me/plan.vwx","to":"/Users/me/plan-2.vwx"}'
# {"document":"/Users/me/plan.vwx","path":"/Users/me/plan-2.vwx"}

# 図面を閉じる
vw2026 call close '{"document":"/Users/me/plan-2.vwx"}'
# {"closed":true}

# セッションを終了する
vw2026 session end
# {"ended":true}
```

- **`close` は保存されていない変更を破棄します。確認ダイアログは表示されません。** 残したい変更は、先に `save` で
  新しいファイルへ保存します（`save` は既にあるファイルへは保存しません）。
- `quit`（`vw2026 call quit`）は、図面をすべて閉じてから呼びます。図面が開いていれば終了せず、開いている図面の
  一覧を返します。Vectorworks が終了すると、セッションも終了します。

コマンドの指定・出力・終了コードは `vw2026 --help` と `vw2026 help <コマンド>` に、道具ごとの引数と結果は
[docs/plugin/tools.md](docs/plugin/tools.md) にあります。

## 更新とアンインストール

更新は、Vectorworks を終了してから行います（Vectorworks の動作中は変更を加えません）。

```bash
vw2026 install cli@latest
# {"outcome":"installed","name":"cli","requested":"latest","tag":"…",…}（新しい版が無ければ "outcome":"up_to_date"）
```

アンインストールは、インストールに使ったスクリプトに `uninstall` を渡して行います。インストールされている版に
合ったスクリプトのコマンドは、`vw2026 uninstall cli` が表示します（このコマンド自体は何も変更しません）。
表示された 1 つ目のコマンド（インストールされているタグのスクリプト）を、**表示されたとおりに**コピーして
実行します。下の出力のタグ `0.0.0-1a2b3c4` は例で、実際には、インストールされている版のタグが表示されます。
実行するコマンドの `<タグ>` は、表示されたタグに置き換えます（そのままでは実行できません）。
既定と違うプラグインのフォルダにインストールしたときは、コマンドに `--plugins-dir` も付いて表示されます。
出力の `<Plug-Ins>` には、実際のプラグインのフォルダの絶対パスが表示されます。コマンドの行の字下げは、実際の
出力ではタブです。JSON の出力と終了コード
（この場合は 2）は `vw2026 help uninstall` にあります。

インストールされている版のリリースが無くなっているとき（プレリリースは、その PR を閉じると削除されます）だけ、
表示された 2 つ目のコマンド（`latest` のスクリプト）を使います。

macOS:

```bash
vw2026 uninstall cli
# 標準エラー:
# vw2026: vw2026 runs from the folder it would uninstall, <Plug-Ins>/cli; nothing is removed. vw2026 is not used once cli is uninstalled; run the first-time script of the installed tag instead:
#     curl -fsSL https://github.com/min-nano/vectorworks-plugin-command-line-interface/releases/download/0.0.0-1a2b3c4/get-vw2026.sh | sh -s -- uninstall
# If that release no longer exists (a prerelease is deleted when its pull request closes), use latest:
#     curl -fsSL https://github.com/min-nano/vectorworks-plugin-command-line-interface/releases/latest/download/get-vw2026.sh | sh -s -- uninstall
# 標準出力:
# {"outcome":"failed","name":"cli","message":"…","command":"curl -fsSL …/download/0.0.0-1a2b3c4/get-vw2026.sh | sh -s -- uninstall"}

# 表示された 1 つ目のコマンドを、表示されたとおりに実行する（<タグ> は表示されたタグ。このままでは実行できない）
curl -fsSL https://github.com/min-nano/vectorworks-plugin-command-line-interface/releases/download/<タグ>/get-vw2026.sh | sh -s -- uninstall
# {"outcome":"uninstalled","name":"cli","hook":{…},"remaining":[],"left":[]}
```

Windows:

```powershell
vw2026 uninstall cli
# 標準エラー:
# vw2026: vw2026 runs from the folder it would uninstall, <Plug-Ins>\cli; nothing is removed. vw2026 is not used once cli is uninstalled; run the first-time script of the installed tag instead:
#     & ([scriptblock]::Create((irm https://github.com/min-nano/vectorworks-plugin-command-line-interface/releases/download/0.0.0-1a2b3c4/get-vw2026.ps1))) uninstall
# If that release no longer exists (a prerelease is deleted when its pull request closes), use latest:
#     & ([scriptblock]::Create((irm https://github.com/min-nano/vectorworks-plugin-command-line-interface/releases/latest/download/get-vw2026.ps1))) uninstall
# 標準出力:
# {"outcome":"failed","name":"cli","message":"…","command":"& ([scriptblock]::Create((irm …/download/0.0.0-1a2b3c4/get-vw2026.ps1))) uninstall"}

# 表示された 1 つ目のコマンドを、表示されたとおりに実行する（<タグ> は表示されたタグ。このままでは実行できない）
& ([scriptblock]::Create((irm https://github.com/min-nano/vectorworks-plugin-command-line-interface/releases/download/<タグ>/get-vw2026.ps1))) uninstall
# {"outcome":"uninstalled","name":"cli","hook":{…},"remaining":[],"left":[]}
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
| [docs/format.md](docs/format.md) | 整形規則と整形ツール（`scripts/format.sh`） |
| [docs/lint.md](docs/lint.md) | 検査ツール（`scripts/lint.sh`） |
| [docs/sdk-research.md](docs/sdk-research.md) | SDK リファレンスでの調査の索引と、待っているもの |
| [docs/design-review.md](docs/design-review.md) | 設計のレビューの指摘のうち、まだ決まっていないもの |
