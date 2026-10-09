# 構成

## 読み込み

```
Vectorworks ──読み込む──▶ cli.vwlibrary / cli.vlb     … 起動時に 1 度だけ。終了まで降ろさない
```

- **更新は Vectorworks を終了してから行います**（[更新](install-and-update.md#更新vw2026-update)）。
- 新しい版は次の起動から効きます。版を確かめるには `vw2026 status` の `version` を見ます。

## 入口

| 入口 | 処理 |
| --- | --- |
| `plugin_module_main`（起動時） | VCOM の初期化・SDK のコールバックを記憶・**OS タイマーの開始**（拡張機能は登録しない） |
| OS タイマー（250 ms ごと） | 見送りの判定 → 受け付け 1 回（`core::serve`）→ `quit` が頼まれていたら終了を頼む |

OS タイマーは起動時に開始します（最初の 10 秒は受け付けない）。

## 受け付けの流れ

```
OS タイマー
  ├─ gSDK が無い・開始から 10 秒以内 → 何もしない
  ├─ undo の記録が開いている（IsCurrentlyBuildingAnUndoEvent）→ 見送る
  ├─ 受け付けの入れ子（受け付け中にまた呼ばれた）→ 見送る
  └─ core::serve（受け付け 1 回）
        ├─ 別の pid の印が down でない → 待機（取り出さない・印を書かない）
        ├─ 要求を名前の昇順で rename で確保して取り出す（1 回 16 件まで。失敗は飛ばす）
        ├─ 道具を実行して応答を書く（quit も応答を書いてから、終了の依頼を返す）
        └─ 生存の印を 2 秒ごとに書き直す
     quit が頼まれていたら CloseAllFilesAndQuitVectorworks
```

- **SDK 呼び出しはメインスレッドからだけ**行います。受け付け用のスレッドは持ちません。
- **受け付けの中にループを書きません**（図面が操作できなくなる）。長く走る道具は、走る前に
  生存の印へ `busy` / `busy_id` / `busy_until` を書きます。
- **終了の依頼は受け付けの外で行います。** `CloseAllFilesAndQuitVectorworks` は保存の確認を
  出すので、応答を書き終え、受け付けから戻ってからタイマーが呼びます（[道具「quit」](tools.md#quit)）。
- タイマーの実装は元:`src/Extensions/ExtMcpPalette.cpp` の `StartMcpBridgeClock` / `ClockTick` /
  `ServeOnce` を移します。mac は `CFRunLoopTimer` を
  **`kCFRunLoopDefaultMode` にだけ**登録します（common modes だとモーダルダイアログの最中にも
  呼ばれ、開いている undo の記録へ書き込みが混ざる）。Windows はスレッドタイマー
  （`SetTimer(nullptr, 0, 250, …)`）。経緯は元:`docs/dev-notes/milestones/m41-bridge-os-timer.md`。

## 受け付けを止めない・処理中は見送る

**利用者が受け付けを止める手段は持ちません**（止めたければアンインストールする）。代わりに、
Vectorworks の処理の妨げにならないよう、次のときはその回の受け付けを**見送ります**。

| 見送る条件 | 理由 |
| --- | --- |
| undo の記録が開いている（`gSDK->IsCurrentlyBuildingAnUndoEvent()`） | その間の書き込みは開いている記録へ混ざる。VW のモーダルダイアログの最中はここに当たる（[Findings「Timers and Notifications」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Timers%20and%20Notifications.md) の 6・7） |
| 受け付けの入れ子 | 受け付けのコードがスタックに載っている（長く走る道具の最中など） |
| 開始から 10 秒以内 | Vectorworks の起動処理と重ねない |

- 何も要求が無いときの 1 回の費用は、スプールの一覧を読むことと、2 秒に 1 回の生存の印の
  書き直しだけです。利用者がドラッグやレンダリングをしている最中にも刻みは届きますが、
  そのとき VW は undo の記録を開いていないので、読む道具はそのまま応えます（同 Findings の表）。
- mac は `kCFRunLoopDefaultMode` にだけ登録するので、モーダルの最中はそもそも刻みません。
  **Windows の `SetTimer` はモーダルの最中も刻む**ので、上の undo の条件が見送りを担います。
- 見送った要求は消さずに残るので、次に受け付けたときに処理されます（呼ぶ側の待ち時間の内なら
  応答が届く。過ぎれば呼ぶ側が取り下げる）。
- 見送りの間は生存の印も書き直されません。15 秒を超えると印は古びますが、呼ぶ側は `pid` の
  プロセスが動いていれば「応えない（`unresponsive`）」と判定し、「止まっている」とは誤りません
  （[作法「生存の判定」](../protocol.md#生存の判定)）。

## 拡張機能を登録しない

**メニューもパレットも PIO も登録しません。** 操作はすべて CLI から行い、更新も CLI が行う
（[更新](install-and-update.md#更新vw2026-update)）ので、利用者の入口は要りません。

- プラグインの読み込みと `plugin_module_main` の呼び出しは、拡張機能の有無に関わらず起きる
  見込みです（PIO だけのプラグインもあるように、読み込みは登録の種類に依らない）。ただし
  **拡張機能を 1 つも登録しない形は実機で確かめていません**（SDK リファレンスにも記載が無い）。
  段 2（骨格）の実機確認で、起動後に `vw2026 status` が `live:true` になることで確かめます。
- **確かめられなかったときの代わり**: 拡張機能を 1 つだけ登録します。メニューコマンドは
  ワークスペースに加えない限り利用者の画面に出ないので、登録しても目に触れません。
  そのときは UUID とユニバーサル名を [識別子](identifiers.md) に足します。

## ソースの配置

```
src/
├─ PluginPrefix.h / BuildConfig.h          … PCH・名前と識別子
├─ ModuleMain.cpp                          … 入口: VCOM の初期化・タイマーの開始
├─ Clock.{h,cpp}                           … OS タイマー・見送りの判定・終了の依頼
├─ core/                                   … SDK に依らない（Json・Bridge・Serve）。プラグインとテストがリンク
└─ tools/                                  … 道具の表（ToolTable.cpp）と中身（SDK 依存）
scripts/                                   … vw-install / vw-uninstall（.sh / .ps1）
resources/                                 … cli.vwr / cli_dev.vwr（拡張機能を登録しないので最小限）
tests/                                     … 無 SDK の単体テスト・スクリプトのテスト
protocol/fixtures/                         … 作法の見本（C++ と Go の両方のテストが読む）
cli/                                       … vw2026（Go）。更新（update）もここ
```

- 名前空間は `VwCli`（`VwCli::core` / `VwCli::tools`）。図面にも配布物にも現れない内部の綴りで、
  改名しても据え置きます。
- 元の `draw/` に当たるものは `tools/` にします（このプラグインは描画しない）。
- スプールの場所と持ち主・権限の確かめは `core/Bridge` が持ちます。
