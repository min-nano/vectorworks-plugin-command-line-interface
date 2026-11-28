# 構成

## 読み込み

```
Vectorworks ──読み込む──▶ cli.vwlibrary / cli.vlb     … 起動時に 1 度だけ。終了まで降ろさない
```

- **更新は Vectorworks の終了後に行います**（[インストール](install-and-update.md#インストールと更新vw2026-install)）。
- 新しい版は次回の起動から有効になります。版を確認するには `vw2026 call ping` の `version` を参照します。

## エントリポイント

| エントリポイント | 処理 |
| --- | --- |
| `plugin_module_main`（起動時） | VCOM の初期化・SDK のコールバックの記憶・**OS タイマーの開始**（拡張機能は登録しない） |
| OS タイマー（250 ms ごと） | 1 回の受付処理（`core::Server::serve`）→ `quit` による終了の依頼があれば終了を要求する（手順は `core::tick`）。スプールの場所が判明した最初の周期処理で下記の通知を登録する（登録できなければ次回の周期処理で再試行する。受付処理の入れ子の周期処理と終了の依頼の最中は登録しない。いずれも SDK の呼び出しの途中であるため。手順は `core::Lifecycle::registerNotification`） |
| 通知 `kNotifyBeforePendingUpdate`（Vectorworks のアイドルから） | 最初の 1 回で「起動完了」のフラグを立てるのみ（以後は何もしない。ここから終了を要求しない）。`quit` はこのフラグが立つまで拒否する（[ツール「quit」](tools.md#quit)。[SDK リファレンス #317](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/317)） |

**受付処理は OS タイマーが 1 回ずつ呼び出します。** ISDK に周期的に呼び出される口は無く、OS のタイマーからは `gSDK` を
読み書きできます（[Findings「Timers and Notifications」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Timers%20and%20Notifications.md) の 1・4）。
通知は `plugin_module_main` ではなく最初の周期処理で登録します（Windows の Vectorworks は起動時にモジュールを再読み込みするので、
固定する前に渡した関数のアドレスは残らない。下記「Windows ではタイマーの作成前にモジュールを固定します」）。通知は起動の完了後も
短い間隔で届き続けるので、その中ではフラグを立てるのみとします（同 4 の 9「起動を終えた目印」）。

## 受付処理の流れ

```
OS タイマー（1 回の周期処理。core::tick）
  ├─ gSDK が存在しない → 何も行わない
  ├─ core::Server::serve（1 回の受付処理。終了の依頼の最中であれば Quitting、それ以外は Idle を渡す）
  │     回の始めに列挙した要求を取り出し、ツールを実行して応答を書き込む
  └─ quit が終了を依頼していれば、終了の依頼の最中のフラグを立てて CloseAllFilesAndQuitVectorworks。戻ったら下ろす
```

`serve` 内の手順（ロック・取り出し・待機標識・占有・処理中の判定・ツールの実行）は
[ブリッジ「1 回の受付処理」](bridge.md#1-回の受付処理coreserverserve)にあります。

- **SDK 呼び出しはメインスレッドからのみ**行います。受付処理用のスレッドは保持しません。
- **受付処理の中で非同期処理の完了を待機しません。** 同期の呼び出しに長時間を要するツールは保持します
  （規則と理由は[通信規則「予約ツール」](../protocol.md#予約ツール)）。
- **終了の依頼は受付処理の外で行います。** `CloseAllFilesAndQuitVectorworks` は図面が開いていると
  保存の確認を表示するので、応答の書き込みを終え、受付処理から戻った後にタイマーが呼び出します（[ツール「quit」](tools.md#quit)）。
  **`quit` は図面が 1 枚でも開いていれば先に拒否します**（保存済みの図面の未保存の変更は読み取れないため。
  図面を閉じるのは呼び出し元。[ツール「quit」](tools.md#quit)）。
- タイマーは、mac では `CFRunLoopTimer` を **`kCFRunLoopCommonModes` に**登録し、Windows では
  スレッドタイマーとします。mac で `kCFRunLoopDefaultMode` のみに登録すると、通常の操作中でも
  レンダリング中でも周期処理が長時間停止します（[Findings「Timers and Notifications」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Timers%20and%20Notifications.md) の 4 の 7）。
- **Windows ではタイマーの作成前にモジュールを固定します。** Windows の Vectorworks は起動時にモジュールを
  一度アンロードして再読み込みするので、固定せずに作成したタイマーは既に存在しないアドレスを呼び出し続け、起動の途中で異常終了します。
  mac は再読み込みしないので固定しません（[Findings「Plug-in Modules」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Plug-in%20Modules.md)
  の「【罠】Windows の VW は起動時にモジュールを一度降ろして読み込み直す」。手順は `src/Clock.cpp`）。
- **間隔は保証されません**（250 ms で登録しても数秒空くことがある。同 4 の 2）。周期処理の間隔に
  依存する処理（時間の計測など）は持たず、時刻は時計から読み取ります。
- **終了の途中もタイマーは停止しません。** 周期処理から終了を要求した後、mac では呼び出しが戻った後に周期処理が
  1〜数回届きますが、`GetOpenFilesList()`・`GetCurrentLayer()` などは異常終了せずに戻ります。Windows では呼び出しの
  後に周期処理は届きません。利用者がメニューの「終了」で終了する経路でも、Windows は `kNotifyBeforeApplicationQuit` の
  後に周期処理が届きません（同 4 の 9。[SDK リファレンス #230](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/230)・
  [#269](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/269)）。mac のこの経路は SDK リファレンスでは
  測定しておらず、本プラグインの実機の確認で異常終了せずに終了することを確認したのみです。
- **図面のハンドル（レイヤなど）は周期処理をまたいで保持しません。** 周期処理の度に取得し直します。
  閉じた文書のレイヤと、後で開いた別の文書のレイヤが同一のアドレスで返ることがあり、持ち越すと
  別の文書を指しうるためです（同 4 の 8）。

## 受付処理を停止させない

**利用者が受付処理を停止する手段は設けません**（停止したい場合はアンインストールする）。要求を受付処理の
外に蓄積することもしません。Vectorworks の処理の妨げになる要求は、次の「Vectorworks が処理中」で
即座に拒否します。

- 要求が無い場合の 1 回の費用は、スプールの一覧の読み取りのみです（SDK を呼び出さない）。
- 起動の直後も最初の周期処理から受け付けます（待機時間は設けない）。起動画面の時期の周期処理から
  `GetCurrentLayer()` と `GetOpenFilesList()` を呼び出して異常終了しないことは確認済みです（[Findings「Timers and Notifications」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Timers%20and%20Notifications.md) の 4 の 8）。
- Vectorworks が周期処理を配信しない間（重い処理・一部のダイアログの最中）は応答しません。その間、要求は
  スプールに残り、周期処理が再開すれば処理されます（呼び出し元の待機時間内なら応答が届く。待機時間を過ぎた要求・呼び出し元が
  強制終了された要求は、待機標識を取得できる（又は存在しない）ので実行しない。[通信規則「待機標識」](../protocol.md#待機標識idwait)）。
  その間もロックは保持したままです（呼び出し元から見た扱いは[通信規則「生存の判定」](../protocol.md#生存の判定)）。

## Vectorworks が処理中

次の間は、要求を `busy` で拒否します。**拒否する要求の範囲とその理由（呼び出し元から見た規則）は
[通信規則「Vectorworks が処理中」](../protocol.md#vectorworks-が処理中)が真実で、ここは判定の配置場所と SDK 側の根拠です。**

| 場面 | 判定 |
| --- | --- |
| 終了の依頼の最中 | `CloseAllFilesAndQuitVectorworks` の前後でフラグを立てて下ろし、その間の周期処理で `core::Server::serve` に `Quitting` を渡す（フラグは `core::Lifecycle` が保持する。[ブリッジ「分割の方針」](bridge.md#分割の方針)） |
| 受付処理の入れ子 | `core::Server` が `serve` の途中かを保持する |

- **受付処理の入れ子**は、ツールの SDK 呼び出しが内部でイベントを処理したとき（モーダルダイアログ・進捗の
  `DoYield` など）に、そのループの中で次の周期処理が届いて発生します（[Findings「Timers and Notifications」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Timers%20and%20Notifications.md) の 4）。
  `close` は保存の確認を表示しないのでイベントを処理しません。`save` は、保存先のフォルダに `VW Backup` を作成できないときの警告で
  イベントを処理しえます（[ツール「図面を操作するツール」](tools.md#図面を操作するツールdocumentssaveclose)）。ダイアログの表示されない保存と
  閉じる処理は、大きな図面でも最中にイベントを処理しません
  （[SDK リファレンス #290](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/290)）。
- **本プラグインのタイマーは 1 本なので、受付処理の入れ子が発生するのは Windows のみです。** Windows はダイアログの
  メッセージループが `WM_TIMER` を配信し、呼び出したタイマー自身が再入します。mac の `CFRunLoopTimer` は自身の処理の
  最中に同一のタイマーを呼び出さないので、ツール内のダイアログの間は周期処理が実行されず、要求は応答されないままスプールに残ります
  （同 4 の 9）。
- **終了の依頼の最中の判定は、Findings が推奨する「終了の呼び出しの最中」のフラグに当たります。** 保存の確認の中で 2 つ目の
  `CloseAllFilesAndQuitVectorworks` を呼び出すと確認が重複して表示され、Windows では呼び出したタイマー自身が確認の最中に
  再入することが判明しました（[SDK リファレンス #230](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/230)。同 4 の 9）。
  Windows では再入した周期処理から 2 つ目を呼び出せてしまうので、判定を廃止する方法は採れません。mac は呼び出したタイマーが
  確認の間は周期処理を実行しないので、この判定が有効なのは Windows のみです（mac は確認の間、何も応答しない）。
  - フラグは**呼び出しが戻ったら下ろします**。戻り値が無く、取り消されたか（Vectorworks が動作し続けるか）を判別できないので、
    下ろさないと取り消した後も拒否し続けるためです。mac では終了していく途中にも周期処理が届きますが、そこから 2 度目を
    呼び出しても何も起きないことが判明しています（[SDK リファレンス #313](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/313)。
    Windows は終了していく回は戻った後に周期処理が届かない）。
  - 終了の依頼は `serve` の外（タイマーが `serve` から戻った後）で行うので、保存の確認の間に再入した周期処理（Windows）は
    受付処理の入れ子にはなりません。それでも `CloseAllFilesAndQuitVectorworks` という SDK 呼び出しの途中なので、
    受付処理の入れ子と同様にツールに SDK を呼び出させません（確認の最中に読み取れることを確認済みなのは
    `GetCurrentLayer()`・`GetOpenFilesList()` などに限られ、閉じようとしている図面を辿る読み取りは測定していない）。
- **undo の記録が開いているかは確認しません。** 周期処理の中から記録が開いて見えた実測は、いずれもプラグイン
  自身の呼び出しが開いて閉じずに戻した記録でした（Findings の 6 はプローブの `DeleteObject` が開いた
  記録、[Undo](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Undo.md) の PIO・`CreateLocus`・スクリプトエンジンもプローブが呼び出したもの）。
  Vectorworks の GUI の操作は、周期処理が入らない区間で記録を開いて閉じます（ドラッグ中・レンダリング中の
  周期処理でも開いていなかった。同 Findings の 7）。本プラグインの書き込みツールは自身で記録を開き、
  戻る前に閉じる（[ブリッジ「ツールの一覧」](bridge.md#ツールの一覧)）ので、記録が混在することはありません。
  他のプラグインが閉じ忘れた記録には備えません（CLI の使用中は GUI を操作しない前提）。

## 拡張機能の非登録

**メニューもパレットも PIO も登録しません。** 操作は全て CLI から行い、更新も CLI が行う
（[インストール](install-and-update.md#インストールと更新vw2026-install)）ので、利用者向けのエントリポイントは不要です。

- **拡張機能を 1 つも登録しなくても、Vectorworks は読み込み、`plugin_module_main` を呼び出します**
  （Vectorworks 2026 の実機の mac と Windows で、起動の直後から `vw2026 status` が `running:true` になり
  `call ping` が応答することを確認した）。
- **初回の起動で利用者の許可を要することがあります（mac も Windows も）。** Vectorworks は開発元の
  不明なプラグインを既定では読み込まず、有効化の記録が無い場合は、起動の途中で
  「開発元不明のプラグイン」のダイアログを表示します。そこで `cli` を「有効化」すると、次回の起動から
  読み込まれます。「有効化」はモジュールのフルパスで記憶されるので、`install` が同一のパスへ差し替える
  更新では許可は不要です。但し記録はダイアログを閉じる度にその時点で存在したプラグインのみで
  書き直されるので、`cli` が存在しない状態で Vectorworks を起動してダイアログを閉じると、同一のパスでも
  再度許可を要します
  （[SDK の調査 0](../sdk-research.md#0-拡張機能を-1-つも登録しないプラグインの読み込み判明)）。
- **全てのプラグインが有効でも、ダイアログは約 24 時間おきに表示されます。プラグインと CLI は、そのダイアログを
  独断で閉じず、日時の書き換えで表示を抑止することもしません。** ダイアログのどのボタンを押すかは呼び出し元が
  選択します（「無効にする」を押すと、次回の起動から `cli` 自身が除外されうる）。
- **ダイアログの最中も、周期処理から図面を操作できます。** その間は Vectorworks が起動を完了しておらず、UI からは
  図面を操作できませんが、プラグインは読み込み済みで、周期処理から図面を開く・新しい図面を作成する・書き込む（undo の記録の中で）・保存する・閉じる処理が成功することを
  確認済みです（mac も Windows も）。プラグインが開いた図面はダイアログを閉じた後も開いたままで、
  Vectorworks はその上にホーム画面を表示して起動を完了します。図面は利用者に見える状態で残り、
  プラグインが黙って閉じることはありません。mac でそうなるのは、タイマーを共通のモードに登録して
  いるためです（ダイアログはモーダルで、既定のモードのタイマーは周期処理を実行しない）。起動の完了を
  前提とする操作（UI の状態・作業画面に依存するもの）は、ダイアログの最中には確認していません
  （[Findings「Plug-in Modules」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Plug-in%20Modules.md)の「ダイアログの最中も、プラグインは刻みから図面を扱える」「mac も、共通モードの刻みからなら扱える」）。

## ソースの配置

```
CMakeLists.txt                             … 共通部・テスト・ファジング・プラグインのビルド
src/
├─ PluginPrefix.h / BuildConfig.h          … PCH・名前と識別子
├─ ModuleMain.cpp                          … エントリポイント: VCOM の初期化・タイマーの開始
├─ Clock.{h,cpp}                           … OS タイマー・終了の依頼
├─ Module-Info.plist.in                    … mac のバンドルの Info.plist（版の鍵を持たない）
├─ core/                                   … SDK に依存しない（Protocol・Json・Files・Bridge・Serve・Lifecycle・Ping・Quit など）。プラグインとテストがリンク
└─ tools/                                  … ツールの一覧（ToolTable.cpp）と実装（Ping.cpp など。SDK 依存。SDK に依存しない部分は core へ）
scripts/                                   … 整形と検査・CI の補助・初回スクリプトの雛形（get-vw）・cli のフック（hooks/）
resources/                                 … cli.vwr（拡張機能を登録しないので最小限。Strings/cli.vwstrings に未使用の文字列 1 つのみ）
tests/                                     … 無 SDK の単体テスト・ファジングのエントリポイント・スクリプトのテスト
protocol/fixtures/                         … 通信規則のフィクスチャ（C++ と Go の両方のテストが読み込む。docs/fixtures.md）
cli/                                       … vw2026・vw2025（Go）。インストールと更新・アンインストール・既定のレジストリもここ
```

- 名前空間は `VwCli`（`VwCli::core` / `VwCli::tools`）。図面にも配布物にも現れない内部の綴りで、
  改名しても据え置きます。
- 各ファイルの分担は[ブリッジ「分割の方針」](bridge.md#分割の方針)にあります。
