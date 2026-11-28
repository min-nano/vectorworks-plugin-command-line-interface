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
| OS タイマー（250 ms ごと） | 受け付け 1 回（`core::Server::serve`）→ `quit` が頼まれていたら終了を頼む（手順は `core::tick`）。スプールの場所が分かった最初の刻みで下の通知を登録する（登録できなければ次の刻みでやり直す。受け付けの入れ子の刻みと終了の依頼の最中は登録しない。どちらも SDK の呼び出しの途中のため。手順は `core::Lifecycle::registerNotification`） |
| 通知 `kNotifyBeforePendingUpdate`（Vectorworks のアイドルから） | 最初の 1 回で「起動を終えた」印を立てるだけ（以後は何もしない。ここから終了を頼まない）。`quit` はこの印が立つまで断る（[道具「quit」](tools.md#quit)。[SDK リファレンス #317](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/317)） |

**受け付けは OS タイマーが 1 回ずつ呼びます。** ISDK に周期的に呼ばれる口は無く、OS のタイマーからは `gSDK` を
読み書きできます（[Findings「Timers and Notifications」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Timers%20and%20Notifications.md) の 1・4）。
通知は `plugin_module_main` ではなく最初の刻みで登録します（Windows の Vectorworks は起動時にモジュールを読み込み直すので、
留める前に渡した関数の番地は残らない。下記「Windows ではタイマーを作る前にモジュールを留めます」）。通知は起動を終えた後も
短い間隔で届き続けるので、中では印を立てるだけにします（同 4 の 9「起動を終えた目印」）。

## 受け付けの流れ

```
OS タイマー（刻み 1 回。core::tick）
  ├─ gSDK が無い → 何もしない
  ├─ core::Server::serve（受け付け 1 回。終了の依頼の最中なら Quitting、他は Idle を渡す）
  │     回の始めに並べた要求を取り出し、道具を実行して応答を書く
  └─ quit が終了を頼んでいたら、終了の依頼の最中の印を立てて CloseAllFilesAndQuitVectorworks。戻ったら下ろす
```

`serve` の中の手順（ロック・取り出し・待つ印・占有・処理中の判定・道具の実行）は
[ブリッジ「受け付け 1 回」](bridge.md#受け付け-1-回coreserverserve)にあります。

- **SDK 呼び出しはメインスレッドからだけ**行います。受け付け用のスレッドは持ちません。
- **受け付けの中で非同期の処理の完了を待ちません。** 同期の呼び出しが長く掛かる道具は持ちます
  （規則と理由は[通信規則「予約された道具」](../protocol.md#予約された道具)）。
- **終了の依頼は受け付けの外で行います。** `CloseAllFilesAndQuitVectorworks` は図面が開いていると
  保存の確認を出すので、応答を書き終え、受け付けから戻ってからタイマーが呼びます（[道具「quit」](tools.md#quit)）。
  **`quit` は図面が 1 枚でも開いていれば先に断ります**（保存済みの図面の未保存の変更は読めないため。
  図面を閉じるのは呼ぶ側。[道具「quit」](tools.md#quit)）。
- タイマーは、mac は `CFRunLoopTimer` を **`kCFRunLoopCommonModes` に**登録し、Windows は
  スレッドタイマーにします。mac で `kCFRunLoopDefaultMode` にだけ登録すると、普通の操作でも
  レンダリング中でも長く刻みが止まります（[Findings「Timers and Notifications」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Timers%20and%20Notifications.md) の 4 の 7）。
- **Windows ではタイマーを作る前にモジュールを留めます。** Windows の Vectorworks は起動時にモジュールを
  一度降ろして読み込み直すので、留めずに作ったタイマーはもう無い番地を呼び続け、起動の途中で落ちます。
  mac は読み込み直さないので留めません（[Findings「Plug-in Modules」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Plug-in%20Modules.md)
  の「【罠】Windows の VW は起動時にモジュールを一度降ろして読み込み直す」。手順は `src/Clock.cpp`）。
- **間隔は保証されません**（250 ms で登録しても数秒空くことがある。同 4 の 2）。刻みの間隔に
  頼る処理（時間の計測など）は持たず、時刻は時計から読みます。
- **終了の途中もタイマーは止めません。** 刻みから終了を頼んだ後、mac では呼び出しが戻った後に刻みが
  1〜数回届きますが、`GetOpenFilesList()`・`GetCurrentLayer()` などは落ちずに返ります。Windows では呼び出しの
  後に刻みは来ません。利用者がメニューの「終了」で終える道でも、Windows は `kNotifyBeforeApplicationQuit` の
  後に刻みが来ず（同 4 の 9。[SDK リファレンス #230](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/230)・
  [#269](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/269)）。mac のこの道は SDK リファレンスでは
  測っておらず、このプラグインの実機の確認で落ちずに終わることを見ただけです。
- **図面のハンドル（レイヤなど）は刻みをまたいで持ちません。** 刻みの度に引き直します。
  閉じた文書のレイヤと、後で開いた別の文書のレイヤが同じアドレスで返ることがあり、持ち越すと
  別の文書を指しうるためです（同 4 の 8）。

## 受け付けを止めない

**利用者が受け付けを止める手段は持ちません**（止めたければアンインストールする）。要求を受け付けの
外に溜め込むこともしません。Vectorworks の処理の妨げになる要求は、次の「Vectorworks が処理中」で
すぐに断ります。

- 何も要求が無いときの 1 回の費用は、スプールの一覧を読むことだけです（SDK を呼ばない）。
- 起動の直後も最初の刻みから受け付けます（待つ時間は持たない）。起動画面の頃の刻みから
  `GetCurrentLayer()` と `GetOpenFilesList()` を呼んで落ちないことは確かめてあります（[Findings「Timers and Notifications」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Timers%20and%20Notifications.md) の 4 の 8）。
- Vectorworks が刻みを配らない間（重い処理・一部のダイアログの最中）は応えません。そのとき要求は
  スプールに残り、刻みが戻れば処理されます（呼ぶ側の待ち時間の内なら応答が届く。過ぎた・呼ぶ側が
  強制終了された要求は、待つ印を掴める（又は無い）ので実行しない。[通信規則「待つ印」](../protocol.md#待つ印idwait)）。
  その間もロックは掴んだままです（呼ぶ側から見た扱いは[通信規則「生存の判定」](../protocol.md#生存の判定)）。

## Vectorworks が処理中

次の間は、要求を `busy` で断ります。**断る要求の範囲とその理由（呼ぶ側から見た規則）は
[通信規則「Vectorworks が処理中」](../protocol.md#vectorworks-が処理中)が真実で、ここは判定の置き場所と SDK の側の根拠です。**

| 場面 | 判定 |
| --- | --- |
| 終了の依頼の最中 | `CloseAllFilesAndQuitVectorworks` の前後で印を立てて下ろし、その間の刻みで `core::Server::serve` に `Quitting` を渡す（印は `core::Lifecycle` が持つ。[ブリッジ「分け方」](bridge.md#分け方)） |
| 受け付けの入れ子 | `core::Server` が `serve` の途中かを持つ |

- **受け付けの入れ子**は、道具の SDK 呼び出しが中でイベントを回したとき（モーダルダイアログ・進捗の
  `DoYield` など）に、そのループの中で次の刻みが届いて起きます（[Findings「Timers and Notifications」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Timers%20and%20Notifications.md) の 4）。
  `close` は保存の確認を出さないので回しません。`save` は、保存先のフォルダに `VW Backup` を作れないときの警告で
  回しえます（[道具「図面を扱う道具」](tools.md#図面を扱う道具documentssaveclose)）。ダイアログの出ない保存と
  閉じるは、大きな図面でも最中にイベントを回しません
  （[SDK リファレンス #290](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/290)）。
- **このプラグインのタイマーは 1 本なので、受け付けの入れ子が起きるのは Windows だけです。** Windows はダイアログの
  メッセージループが `WM_TIMER` を配り、呼んだタイマー自身が入り直します。mac の `CFRunLoopTimer` は自分の処理の
  最中に同じタイマーを呼ばないので、道具の中のダイアログの間は刻まず、要求は応えられずにスプールに残ります
  （同 4 の 9）。
- **終了の依頼の最中の判定は、Findings が勧める「終了の呼び出しの最中」の印に当たります。** 保存の確認の中で 2 つ目の
  `CloseAllFilesAndQuitVectorworks` を呼ぶと確認が重なって出て、Windows では呼んだタイマー自身が確認の最中に
  入り直すと分かりました（[SDK リファレンス #230](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/230)。同 4 の 9）。
  Windows では入り直した刻みから 2 つ目を呼べてしまうので、判定をやめる道は採れません。mac は呼んだタイマーが
  確認の間は刻まないので、この判定が効くのは Windows だけです（mac は確認の間、何も応えない）。
  - 印は**呼び出しが戻ったら下ろします**。戻り値が無く、取り消されたか（Vectorworks が動き続けるか）を見分けられないので、
    下ろさないと取り消した後も断り続けるためです。mac では終わっていく途中にも刻みが届きますが、そこから 2 度目を
    呼んでも何も起きないと分かっています（[SDK リファレンス #313](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/313)。
    Windows は終わっていく回は戻った後に刻みが来ない）。
  - 終了の依頼は `serve` の外（タイマーが `serve` から戻った後）で行うので、保存の確認の間に入り直した刻み（Windows）は
    受け付けの入れ子にはなりません。それでも `CloseAllFilesAndQuitVectorworks` という SDK 呼び出しの途中なので、
    受け付けの入れ子と同じく道具に SDK を呼ばせません（確認の最中に読めると確かめてあるのは
    `GetCurrentLayer()`・`GetOpenFilesList()` などに限られ、閉じようとしている図面を辿る読み方は測っていない）。
- **undo の記録が開いているかは見ません。** 刻みの中から記録が開いて見えた実測は、どれもプラグイン
  自身の呼び出しが開いて閉じずに戻した記録でした（Findings の 6 はプローブの `DeleteObject` が開いた
  記録、[Undo](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Undo.md) の PIO・`CreateLocus`・スクリプトエンジンもプローブが呼んだもの）。
  Vectorworks の GUI の操作は、刻みが入らない区間で記録を開いて閉じます（ドラッグ中・レンダリング中の
  刻みでも開いていなかった。同 Findings の 7）。このプラグインの書く道具は自分で記録を開き、
  戻る前に閉じる（[ブリッジ「道具の表」](bridge.md#道具の表)）ので、記録が混ざることはありません。
  他のプラグインが閉じ忘れた記録には備えません（CLI を使っている間は GUI を操作しない前提）。

## 拡張機能を登録しない

**メニューもパレットも PIO も登録しません。** 操作は全て CLI から行い、更新も CLI が行う
（[インストール](install-and-update.md#インストールと更新vw2026-install)）ので、利用者の入口は要りません。

- **拡張機能を 1 つも登録しなくても、Vectorworks は読み込み、`plugin_module_main` を呼びます**
  （Vectorworks 2026 の実機の mac と Windows で、起動の直後から `vw2026 status` が `running:true` になり
  `call ping` が応えることを確かめた）。
- **最初の起動で利用者の許可が要ることがあります（mac も Windows も）。** Vectorworks は開発元の
  分からないプラグインを既定では読み込まず、有効化の記録が無いときは、起動の途中で
  「開発元不明のプラグイン」のダイアログを出します。そこで `cli` を「有効化」すると、次の起動から
  読み込まれます。「有効化」はモジュールのフルパスで覚えるので、`install` が同じパスへ付け替える
  更新では許可は要りません。但し記録はダイアログを閉じる度にそのとき居たプラグインだけで
  書き直されるので、`cli` が居ない状態で Vectorworks を起動してダイアログを閉じると、同じパスでも
  もう一度要ります
  （[SDK の調査 0](../sdk-research.md#0-拡張機能を-1-つも登録しないプラグインの読み込み分かった)）。
- **全部のプラグインが有効でも、ダイアログは約 24 時間おきに出ます。プラグインと CLI は、そのダイアログを
  勝手に閉じず、日時の書き換えで出なくすることもしません。** ダイアログのどのボタンを押すかは呼ぶ側が
  選びます（「無効にする」を押すと、次の起動から `cli` 自身が外れうる）。
- **ダイアログの最中も、刻みから図面を扱えます。** その間は Vectorworks が起動を終えておらず、UI からは
  図面を扱えませんが、プラグインは読み込み済みで、刻みから図面を開く・新しい図面を作る・書く（undo の記録の中で）・保存する・閉じるが通ることを
  確かめてあります（mac も Windows も）。プラグインが開いた図面はダイアログを閉じた後も開いたままで、
  Vectorworks はその上にホーム画面を出して起動を終えます。図面は利用者に見える状態で残り、
  プラグインが黙って閉じることはありません。mac でそうなるのは、タイマーを共通のモードに登録して
  いるからです（ダイアログはモーダルで、既定のモードのタイマーは刻まない）。起動が終わっていることを
  前提にする操作（UI の状態・作業画面に依るもの）は、ダイアログの最中には確かめていません
  （[Findings「Plug-in Modules」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Plug-in%20Modules.md)の「ダイアログの最中も、プラグインは刻みから図面を扱える」「mac も、共通モードの刻みからなら扱える」）。

## ソースの配置

```
CMakeLists.txt                             … 共通部・テスト・ファジング・プラグインのビルド
src/
├─ PluginPrefix.h / BuildConfig.h          … PCH・名前と識別子
├─ ModuleMain.cpp                          … 入口: VCOM の初期化・タイマーの開始
├─ Clock.{h,cpp}                           … OS タイマー・終了の依頼
├─ Module-Info.plist.in                    … mac のバンドルの Info.plist（版の鍵を持たない）
├─ core/                                   … SDK に依らない（Protocol・Json・Files・Bridge・Serve・Lifecycle・Ping・Quit など）。プラグインとテストがリンク
└─ tools/                                  … 道具の表（ToolTable.cpp）と中身（Ping.cpp など。SDK 依存。SDK に依らない部分は core へ）
scripts/                                   … 整形と検査・CI の補助・初回のスクリプトの雛形（get-vw）・cli のフック（hooks/）
resources/                                 … cli.vwr（拡張機能を登録しないので最小限。Strings/cli.vwstrings に使わない文字列 1 つだけ）
tests/                                     … 無 SDK の単体テスト・ファジングの入口・スクリプトのテスト
protocol/fixtures/                         … 通信規則の見本（C++ と Go の両方のテストが読む。docs/fixtures.md）
cli/                                       … vw2026・vw2025（Go）。インストールと更新・アンインストール・既定のレジストリもここ
```

- 名前空間は `VwCli`（`VwCli::core` / `VwCli::tools`）。図面にも配布物にも現れない内部の綴りで、
  改名しても据え置きます。
- 各ファイルの受け持ちは[ブリッジ「分け方」](bridge.md#分け方)にあります。
