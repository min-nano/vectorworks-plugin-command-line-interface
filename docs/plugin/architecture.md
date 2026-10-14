# 構成

## 読み込み

```
Vectorworks ──読み込む──▶ cli.vwlibrary / cli.vlb     … 起動時に 1 度だけ。終了まで降ろさない
```

- **更新は Vectorworks を終了してから行います**（[インストール](install-and-update.md#インストールと更新vw2026-install)）。
- 新しい版は次の起動から効きます。版を確かめるには `vw2026 call ping` の `version` を見ます。

## 入口

| 入口 | 処理 |
| --- | --- |
| `plugin_module_main`（起動時） | VCOM の初期化・SDK のコールバックを記憶・**OS タイマーの開始**（拡張機能は登録しない） |
| OS タイマー（250 ms ごと） | 見送りの判定 → 受け付け 1 回（`core::Server::serve`）→ `quit` が頼まれていたら終了を頼む |

OS タイマーは起動時に開始します（最初の 10 秒は受け付けない）。

## 受け付けの流れ

```
OS タイマー
  ├─ gSDK が無い・開始から 10 秒以内 → 何もしない
  ├─ undo の記録が開いている（IsCurrentlyBuildingAnUndoEvent）→ 見送る
  ├─ 受け付けの入れ子（受け付け中にまた呼ばれた）→ 見送る
  └─ core::Server::serve（受け付け 1 回）
        ├─ ロックを掴んでいない → 取ってみる。取れなければ何もしない（次の回に取り直す）
        │                         取れたら前の回の残骸を消す
        ├─ 要求を名前の昇順で読んで消す（回の始めに並べたものだけ。読めない・消せないものは飛ばす）
        ├─ 呼ぶ側の待つ印（<id>.wait）を掴めたら、待つ者が居ないので実行しない
        ├─ 占有の印を発行していれば、その印の要求だけを実行する（tools・ping は誰にでも。quit は占有の中だけ）
        └─ 道具を実行して応答を書く（quit も応答を書いてから、終了の依頼を返す）
     quit が頼まれていたら CloseAllFilesAndQuitVectorworks
```

- **SDK 呼び出しはメインスレッドからだけ**行います。受け付け用のスレッドは持ちません。
- **受け付けの中にループを書きません**（図面が操作できなくなる）。長く走る道具は持ちません
  （[通信規則「予約された道具」](../protocol.md#予約された道具)）。
- **終了の依頼は受け付けの外で行います。** `CloseAllFilesAndQuitVectorworks` は保存の確認を
  出すので、応答を書き終え、受け付けから戻ってからタイマーが呼びます（[道具「quit」](tools.md#quit)）。
- タイマーは、mac は `CFRunLoopTimer` を **`kCFRunLoopCommonModes` に**登録し、Windows は
  スレッドタイマー（`SetTimer(nullptr, 0, 250, …)`）にします。mac で `kCFRunLoopDefaultMode`
  にだけ登録すると、ふつうの操作でも十数秒から数分、レンダリング中は 6 分近く刻みが止まります
  （[Findings「Timers and Notifications」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Timers%20and%20Notifications.md) の 4 の 7）。
- **間隔は保証されません**（250 ms で登録しても、最大 3〜5 秒空く。同 4 の 2）。刻みの間隔に
  頼る処理（時間の計測など）は持たず、時刻は時計から読みます。

## 受け付けを止めない・処理中は見送る

**利用者が受け付けを止める手段は持ちません**（止めたければアンインストールする）。代わりに、
Vectorworks の処理の妨げにならないよう、次のときはその回の受け付けを**見送ります**。

| 見送る条件 | 理由 |
| --- | --- |
| undo の記録が開いている（`gSDK->IsCurrentlyBuildingAnUndoEvent()`） | その間の書き込みは開いている記録へ混ざる。VW のモーダルダイアログの最中はここに当たる（[Findings「Timers and Notifications」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Timers%20and%20Notifications.md) の 6・7）。この条件の是非は[未決 2](open-questions.md#2-undo-の記録が開いている間の見送り) |
| 受け付けの入れ子 | 受け付けのコードがスタックに載っている（道具の中から Vectorworks がイベントを回したときなど） |
| 開始から 10 秒以内 | Vectorworks の起動処理と重ねない |

- 何も要求が無いときの 1 回の費用は、スプールの一覧を読むことだけです。利用者がドラッグやレンダリングをしている最中にも刻みは届きますが、
  そのとき VW は undo の記録を開いていないので、読む道具はそのまま応えます（同 Findings の表）。
- mac（共通モード）も Windows もモーダルダイアログの最中に刻むので、上の undo の条件が
  見送りを担います（VW のモーダルの最中は、刻みの中の `IsCurrentlyBuildingAnUndoEvent()` が
  真を返すことを確かめてある。同 Findings の 6）。
- 見送った要求は消さずに残るので、次に受け付けたときに処理されます（呼ぶ側の待ち時間の内なら
  応答が届く。過ぎた・呼ぶ側が強制終了された要求は、待つ印を掴める（または無い）ので実行しない。[通信規則「待つ印」](../protocol.md#待つ印idwait)）。
- 見送りの間もロックは掴んだままです（呼ぶ側から見た扱いは[通信規則「生存の判定」](../protocol.md#生存の判定)）。

## 拡張機能を登録しない

**メニューもパレットも PIO も登録しません。** 操作はすべて CLI から行い、更新も CLI が行う
（[インストール](install-and-update.md#インストールと更新vw2026-install)）ので、利用者の入口は要りません。

- プラグインの読み込みと `plugin_module_main` の呼び出しは、拡張機能の有無に関わらず起きる
  見込みです（PIO だけのプラグインもあるように、読み込みは登録の種類に依らない）。ただし
  **拡張機能を 1 つも登録しない形は実機で確かめていません**（SDK リファレンスにも記載が無い）。
  段 2（骨格と配布）の実機確認で、起動後に `vw2026 status` が `running:true` になり `call ping` が応えることで確かめます。
- **確かめられなかったときの代わり**: 拡張機能を 1 つだけ登録します。メニューコマンドは
  ワークスペースに加えない限り利用者の画面に出ないので、登録しても目に触れません。
  そのときは UUID とユニバーサル名を [識別子](identifiers.md) に足します。

## ソースの配置

```
CMakeLists.txt                             … 共通部・テスト・ファジング・プラグインのビルド
src/
├─ PluginPrefix.h / BuildConfig.h          … PCH・名前と識別子
├─ ModuleMain.cpp                          … 入口: VCOM の初期化・タイマーの開始
├─ Clock.{h,cpp}                           … OS タイマー・見送りの判定・終了の依頼
├─ core/                                   … SDK に依らない（Protocol・Json・Files・Bridge・Serve）。プラグインとテストがリンク
└─ tools/                                  … 道具の表（ToolTable.cpp）と中身（SDK 依存）
scripts/                                   … 整形と検査（format.sh・lint.sh・powershell.ps1）・get-vw2026（.sh / .ps1。CLI を取って install を走らせるだけ）
resources/                                 … cli.vwr（拡張機能を登録しないので最小限）
tests/                                     … 無 SDK の単体テスト・ファジングの入口・スクリプトのテスト
protocol/fixtures/                         … 通信規則の見本（C++ と Go の両方のテストが読む。docs/fixtures.md）
cli/                                       … vw2026（Go）。インストールと更新（install）・アンインストール（uninstall）もここ
```

- 名前空間は `VwCli`（`VwCli::core` / `VwCli::tools`）。図面にも配布物にも現れない内部の綴りで、
  改名しても据え置きます。
- スプールの場所・ロックは `core/Bridge` が持ちます（ロックは OS の
  呼び出しだけで SDK に依らない。OS ごとの呼び出しは `core/Files*`）。
