# vectorworks-plugin-command-line-interface

[![codecov](https://codecov.io/gh/min-nano/vectorworks-plugin-command-line-interface/graph/badge.svg)](https://codecov.io/gh/min-nano/vectorworks-plugin-command-line-interface)

**Vectorworks をコマンドラインから操作するコマンドと、Vectorworks がコマンドを受信するための Vectorworks 用プラグインです。**
操作の対象は、インストールとライセンス認証を完了した Vectorworks です。`vw2026 call` は、Vectorworks を起動し、
プラグインが動作している間のみ実行できます。
シェルスクリプトや他のプログラム、AI エージェントから、次の操作を実行できます。

- **図面の読み取り**: レイヤとクラスの一覧、レイヤ内の図形（種別・名前・クラス・外形）、種別ごとの図形の数
- **図面の操作**: 開いている図面の一覧、新しいファイルへの保存、図面の終了
- **Vectorworks の操作**: 起動、終了、動作状態の確認
- **プラグインのインストール**: 対応するプラグインのインストール・更新・アンインストール

結果は JSON で標準出力に返され、成否は終了コードで判別できます。

| 対応する Vectorworks | OS | コマンド |
| --- | --- | --- |
| Vectorworks 2026 | mac（Apple シリコン）・Windows（x64） | `vw2026` |
| Vectorworks 2025 | 同上 | `vw2025` |

## インストール

Vectorworks を終了してから、次のコマンドを実行します。

mac（ターミナル）:

```bash
curl -fsSL https://github.com/min-nano/vectorworks-plugin-command-line-interface/releases/latest/download/get-vw2026.sh | sh
# {"outcome":"installed","name":"cli","requested":"<タグ>","tag":"<タグ>",…}
```

Windows（PowerShell）:

```powershell
irm https://github.com/min-nano/vectorworks-plugin-command-line-interface/releases/latest/download/get-vw2026.ps1 | iex
# {"outcome":"installed","name":"cli","requested":"<タグ>","tag":"<タグ>",…}
```

1. **新しい端末を開く**と、`vw2026` を使用できます。mac で `~/.local/bin` が PATH に含まれていない場合は、表示される
   案内に従って PATH に追加します。
2. **Vectorworks を起動します。** 起動の途中で「開発元不明のプラグイン」のダイアログが表示され、`cli` が「無効」と
   示された場合は、`cli` を選択して「有効化」し、Vectorworks を再起動します。
3. 新しい端末で次の結果が返されれば、使用可能です。

   ```bash
   vw2026 status
   # {"running":true,"spool":"…"}
   ```

Vectorworks 2025 にインストールする場合は、URL の `get-vw2026` を `get-vw2025` に置き換えます（コマンドは `vw2025`）。

## 使用方法

結果は JSON で標準出力に返されます。

```bash
# Vectorworks を起動する
vw2026 launch
# {"launched":true,"command":[…]}

# 操作を受け付けているかを確認する（起動を待機する場合は、running が true になるまで繰り返す）
vw2026 status
# {"running":true,"spool":"…"}

# プラグインの版と、図面が開いているかを確認する
vw2026 call ping
# {"version":"abc1234","branch":"main","protocol":…,"pid":4242,"session":false,"document_open":true,"current_layer":"1F"}

# 使用可能なツールの一覧を取得する
vw2026 tools
# {"protocol":…,"tools":[{"name":"layers","description":"…","inputSchema":{…},"kind":"read"},…]}
```

ツールは `vw2026 call <ツール> ['<引数の JSON>']` で呼び出します。

```bash
# 開いている図面の一覧を取得する
vw2026 call documents
# {"documents":[{"name":"plan.vwx","path":"/Users/me/plan.vwx","untitled":false,"active":true}],"count":1}

# レイヤの一覧を取得する
vw2026 call layers
# {"layers":[{"name":"1F","kind":"design","referenced":false,"scale":50,"objects":13,"current":true}],"count":1}

# レイヤ 1F の図形を最大 10 個取得する
vw2026 call layer_objects '{"layer":"1F","limit":10}'
# {"layer":"1F","unit":"mm","objects":[{"index":0,"type":3,"type_name":"box","pio_name":null,"name":"","class":"…","bounds":{…}},…],"returned":10,"total":13}
```

### 図面の保存・終了と Vectorworks の終了

Vectorworks に指示する操作（`save`・`close`・`export_pdf`・`quit`）は、他のプログラムの操作と混在しないように、
**セッションによって占有した状態でのみ**実行できます（占有していない場合・他が占有している場合は、標準出力に何も返さず、終了コード 23 で拒否されます。理由は標準エラーに出力されます）。

```bash
# セッションを開始する
vw2026 session start
# {"session":"<ID>"}

# 以後の呼び出しにセッション ID を付与する（呼び出しごとに --session <ID> を指定してもよい）
export VW2026_SESSION=<ID>

# documents が返した path の図面を、新しいファイルへ保存する
vw2026 call save '{"document":"/Users/me/plan.vwx","to":"/Users/me/plan-2.vwx"}'
# {"document":"/Users/me/plan.vwx","path":"/Users/me/plan-2.vwx"}

# シートレイヤ（layers の kind が sheet）を、この順に 1 枚 1 頁で PDF へ書き出す（mac のみ）
vw2026 call export_pdf '{"document":"/Users/me/plan-2.vwx","sheets":["S-1","S-2"],"to":"/Users/me/plan-2.pdf"}'
# {"document":"/Users/me/plan-2.vwx","path":"/Users/me/plan-2.pdf","pages":2}

# 図面を閉じる
vw2026 call close '{"document":"/Users/me/plan-2.vwx"}'
# {"closed":true}

# セッションを終了する
vw2026 session end
# {"ended":true}
```

- **`close` は保存されていない変更を破棄します。確認ダイアログは表示されません。** 保持したい変更は、先に `save` で
  新しいファイルへ保存します（`save` は既存のファイルへは保存しません）。
- `export_pdf` も既存のファイルへは書き出しません。書き直す場合は、先にファイルを消すか、新しい `to` を渡します。
- `quit`（`vw2026 call quit`）は、図面を全て閉じてから呼び出します。図面が開いている場合は終了せず、開いている
  図面の一覧を返します。Vectorworks が終了すると、セッションも終了します。

コマンドの指定・出力・終了コードは `vw2026 --help` と `vw2026 help <コマンド>` に、ツールごとの引数と結果は
[docs/plugin/tools.md](docs/plugin/tools.md) に記載されています。

## 更新とアンインストール

更新は、Vectorworks を終了してから行います（Vectorworks の動作中は変更を加えません）。

```bash
vw2026 install cli@latest
# {"outcome":"installed","name":"cli","requested":"latest","tag":"…",…}（新しい版が存在しない場合は "outcome":"up_to_date"）
```

アンインストールは、インストールに使用したスクリプトに `uninstall` を渡して行います。インストールされている版に
対応するスクリプトのコマンドは、`vw2026 uninstall cli` が表示します（このコマンド自体は何も変更しません）。
表示された 1 つ目のコマンド（インストールされているタグのスクリプト）を、**表示された通りに**複製して
実行します。下の出力のタグ `0.0.0-1a2b3c4` は例であり、実際には、インストールされている版のタグが表示されます。
実行するコマンドの `<タグ>` は、表示されたタグに置き換えます（そのままでは実行できません）。
既定とは異なるプラグインのフォルダにインストールした場合は、コマンドに `--plugins-dir` も付加されて表示されます。
出力の `<Plug-Ins>` には、実際のプラグインのフォルダの絶対パスが表示されます。コマンドの行の字下げは、実際の
出力ではタブです。JSON の出力と終了コード
（この場合は 2）は `vw2026 help uninstall` に記載されています。

インストールされている版のリリースが削除されている場合（プレリリースは、その PR を閉じると削除されます）のみ、
表示された 2 つ目のコマンド（`latest` のスクリプト）を使用します。

mac:

```bash
vw2026 uninstall cli
# 標準エラー:
# vw2026: vw2026 runs from the folder it would uninstall, <Plug-Ins>/cli; nothing is removed. vw2026 is not used once cli is uninstalled; run the first-time script of the installed tag instead:
#     curl -fsSL https://github.com/min-nano/vectorworks-plugin-command-line-interface/releases/download/0.0.0-1a2b3c4/get-vw2026.sh | sh -s -- uninstall
# If that release no longer exists (a prerelease is deleted when its pull request closes), use latest:
#     curl -fsSL https://github.com/min-nano/vectorworks-plugin-command-line-interface/releases/latest/download/get-vw2026.sh | sh -s -- uninstall
# 標準出力:
# {"outcome":"failed","name":"cli","message":"…","command":"curl -fsSL …/download/0.0.0-1a2b3c4/get-vw2026.sh | sh -s -- uninstall"}

# 表示された 1 つ目のコマンドを、表示された通りに実行する（<タグ> は表示されたタグ。このままでは実行できない）
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

# 表示された 1 つ目のコマンドを、表示された通りに実行する（<タグ> は表示されたタグ。このままでは実行できない）
& ([scriptblock]::Create((irm https://github.com/min-nano/vectorworks-plugin-command-line-interface/releases/download/<タグ>/get-vw2026.ps1))) uninstall
# {"outcome":"uninstalled","name":"cli","hook":{…},"remaining":[],"left":[]}
```

## 関連文書

開発者向けの文書です。

| ファイル | 内容 |
| --- | --- |
| [docs/design.md](docs/design.md) | 目的・決定事項・構成・安全性の前提 |
| [docs/plugin/](docs/plugin/README.md) | プラグインの設計（構成・ブリッジ・ツール・識別子・ビルド・配布・実機での検証方法・根拠） |
| [docs/protocol.md](docs/protocol.md) | 通信規則（プラグインと呼び出し元の取り決め。真実は本文書） |
| [docs/fixtures.md](docs/fixtures.md) | 通信規則のフィクスチャ（`protocol/fixtures`。C++ と Go のテストが同一のものを読み込む） |
| [docs/registry.md](docs/registry.md) | `vw2026 install` が読み込むレジストリとマニフェストの記法・フック・プラグインの提供側への要件 |
| `vw2026 --help` | `vw2026` のコマンド・指定・終了コード（定義は [cli/cmd/vw2026/](cli/cmd/vw2026/)。`vw2026 help <command>` で各コマンド） |
| [docs/format.md](docs/format.md) | 整形規則と整形ツール（`scripts/format.sh`） |
| [docs/lint.md](docs/lint.md) | 検査ツール（`scripts/lint.sh`） |
| [docs/sdk-research.md](docs/sdk-research.md) | SDK リファレンスでの調査の索引と、調査しないと決定した事項 |

今後作成するもの・未決定の事項・未検証の事項は [issue](https://github.com/min-nano/vectorworks-plugin-command-line-interface/issues) に記載されています。
