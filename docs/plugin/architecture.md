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
| OS タイマー（250 ms ごと） | 受け付け 1 回（`core::Server::serve`）→ `quit` が頼まれていたら終了を頼む（手順は `core::tick`）。最初の刻みで下の通知を登録する（登録できなければ次の刻みでやり直す） |
| 通知 `kNotifyBeforePendingUpdate`（Vectorworks のアイドルから） | 最初の 1 回で「起動を終えた」印を立てるだけ（以後は何もしない。ここから終了を頼まない）。`quit` はこの印が立つまで断る（[道具「quit」](tools.md#quit)。[SDK リファレンス #317](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/317)） |

**受け付けは OS タイマーが 1 回ずつ呼びます。** ISDK に周期的に呼ばれる口は無く、OS のタイマーからは `gSDK` を
読み書きできます（[Findings「Timers and Notifications」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Timers%20and%20Notifications.md) の 1・4）。
OS タイマーは起動時に開始し、最初の刻みから受け付けます。通知は `plugin_module_main` ではなく最初の刻みで登録します
（Windows の Vectorworks は起動時にモジュールを一度降ろして読み込み直すので、留める前の `plugin_module_main` で渡した
関数の番地は残らない。刻みはモジュールを留めてから作ったタイマーから来る。下記「Windows ではタイマーを作る前にモジュールを
留めます」）。通知は起動を終えたあとも 20 ms ほどの間隔で届くので、中では印を立てるだけにします（[Findings「Timers and Notifications」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Timers%20and%20Notifications.md) の 4 の 9「起動を終えた目印」）。

## 受け付けの流れ

```
OS タイマー（刻み 1 回。core::tick）
  ├─ gSDK が無い → 何もしない
  ├─ core::Server::serve（受け付け 1 回。終了の依頼の最中なら Quitting、ほかは Idle を渡す）
  │     回の始めに並べた要求を取り出し、道具を実行して応答を書く
  └─ quit が終了を頼んでいたら、終了の依頼の最中の印を立てて CloseAllFilesAndQuitVectorworks。戻ったら下ろす
```

`serve` の中の手順（ロック・取り出し・待つ印・占有・処理中の判定・道具の実行）は
[ブリッジ「受け付け 1 回」](bridge.md#受け付け-1-回coreserverserve)にあります。

- **SDK 呼び出しはメインスレッドからだけ**行います。受け付け用のスレッドは持ちません。
- **受け付けの中で非同期の処理の完了を待ちません。** 同期の呼び出しが長くかかる道具は持ちます
  （規則と理由は[通信規則「予約された道具」](../protocol.md#予約された道具)）。
- **終了の依頼は受け付けの外で行います。** `CloseAllFilesAndQuitVectorworks` は図面が開いていると
  保存の確認を出すので、応答を書き終え、受け付けから戻ってからタイマーが呼びます（[道具「quit」](tools.md#quit)）。
  **`quit` は図面が 1 枚でも開いていれば先に断ります**（保存済みの図面の未保存の変更は読めないため。
  図面を閉じるのは呼ぶ側。[道具「quit」](tools.md#quit)）。
- タイマーは、mac は `CFRunLoopTimer` を **`kCFRunLoopCommonModes` に**登録し、Windows は
  スレッドタイマー（`SetTimer(nullptr, 0, 250, …)`）にします。mac で `kCFRunLoopDefaultMode`
  にだけ登録すると、ふつうの操作でも十数秒から数分、レンダリング中は 6 分近く刻みが止まります
  （[Findings「Timers and Notifications」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Timers%20and%20Notifications.md) の 4 の 7）。
- **Windows ではタイマーを作る前にモジュールを留めます**（`GetModuleHandleExW` に
  `GET_MODULE_HANDLE_EX_FLAG_FROM_ADDRESS | GET_MODULE_HANDLE_EX_FLAG_PIN`）。Windows の
  Vectorworks は起動時にモジュールを読み込んで `kPluginModuleInit` を送ったあと、`kPluginModuleDeinit`
  を送らずに一度降ろして読み込み直すので、留めずに作ったタイマーはもう無い番地を呼び続け、起動の
  途中で落ちます。留められなければタイマーを作りません。mac は読み込み直さないので留めません
  （[Findings「Plug-in Modules」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Plug-in%20Modules.md)
  の「【罠】Windows の VW は起動時にモジュールを一度降ろして読み込み直す」）。
- **間隔は保証されません**（250 ms で登録しても、最大 3〜5 秒空く。同 4 の 2）。刻みの間隔に
  頼る処理（時間の計測など）は持たず、時刻は時計から読みます。
- **終了の途中もタイマーは止めません。** 刻みから終了を頼んだあと、mac では呼び出しが戻ったあとに刻みが
  1〜数回届きますが、`GetOpenFilesList()`・`GetCurrentLayer()` などは落ちずに返ります。Windows では呼び出しの
  あとに刻みは来ません。利用者がメニューの「終了」で終える道でも、Windows は `kNotifyBeforeApplicationQuit` の
  あとに刻みが来ず（同 4 の 9。[SDK リファレンス #230](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/230)・
  [#269](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/269)）。mac のこの道は SDK リファレンスでは
  測っておらず、このプラグインの実機の確認で落ちずに終わることを見ただけです
  （[SDK の調査「残っているもの」](../sdk-research.md#残っているもの)）。
- **図面のハンドル（レイヤなど）は刻みをまたいで持ちません。** 刻みのたびに引き直します。
  閉じた文書のレイヤと、後で開いた別の文書のレイヤが同じアドレスで返ることがあり、持ち越すと
  別の文書を指しうるためです（同 4 の 8）。

## 受け付けを止めない

**利用者が受け付けを止める手段は持ちません**（止めたければアンインストールする）。要求を受け付けの
外に溜め込むこともしません。Vectorworks の処理の妨げになる要求は、次の「Vectorworks が処理中」で
すぐに断ります。

- 何も要求が無いときの 1 回の費用は、スプールの一覧を読むことだけです（SDK を呼ばない）。
- 起動の直後も最初の刻みから受け付けます。待つ時間（以前の「開始から 10 秒」）は根拠が無いので
  持ちません。起動画面の頃の刻みから `GetCurrentLayer()` と `GetOpenFilesList()` を呼んで落ちない
  ことは確かめてあります（[Findings「Timers and Notifications」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Timers%20and%20Notifications.md) の 4 の 8）。
- Vectorworks が刻みを配らない間（重い処理・一部のダイアログの最中）は応えません。そのとき要求は
  スプールに残り、刻みが戻れば処理されます（呼ぶ側の待ち時間の内なら応答が届く。過ぎた・呼ぶ側が
  強制終了された要求は、待つ印を掴める（または無い）ので実行しない。[通信規則「待つ印」](../protocol.md#待つ印idwait)）。
  その間もロックは掴んだままです（呼ぶ側から見た扱いは[通信規則「生存の判定」](../protocol.md#生存の判定)）。

## Vectorworks が処理中

次の間は、要求を `busy` で断ります。**断る要求の範囲とその理由（呼ぶ側から見た規則）は
[通信規則「Vectorworks が処理中」](../protocol.md#vectorworks-が処理中)が真実で、ここは判定の置き場所と SDK の側の根拠です。**

| 場面 | 判定 |
| --- | --- |
| 終了の依頼の最中 | `CloseAllFilesAndQuitVectorworks` の前後で印を立てて下ろし、その間の刻みで `core::Server::serve` に `Quitting` を渡す（印は `src/core` の `core::Lifecycle` が持ち、`Clock` は SDK を呼ぶだけ。[ブリッジ「分け方」](bridge.md#分け方)） |
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
- **終了の依頼の最中の判定は、Findings が勧める「終了の呼び出しの最中」の旗に当たります。** 保存の確認の中で 2 つ目の
  `CloseAllFilesAndQuitVectorworks` を呼ぶと確認が重なって出て、Windows では呼んだタイマー自身が確認の最中に
  入り直すと分かりました（[SDK リファレンス #230](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/230)。同 4 の 9）。
  Windows では入り直した刻みから 2 つ目を呼べてしまうので、判定をやめる道は採れません。mac は呼んだタイマーが
  確認の間は刻まないので、この判定が効くのは Windows だけです（mac は確認の間、何も応えない）。
  - 印は**呼び出しが戻ったら下ろします**。戻り値が無く、取り消されたか（Vectorworks が動き続けるか）を見分けられないので、
    下ろさないと取り消したあとも断り続けるためです。mac では終わっていく途中にも刻みが届きますが、そこから 2 度目を
    呼んでも何も起きないと分かっています（[SDK リファレンス #313](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/313)。
    Windows は終わっていく回は戻ったあとに刻みが来ない）。
  - 終了の依頼は `serve` の外（タイマーが `serve` から戻ったあと）で行うので、保存の確認の間に入り直した刻み（Windows）は
    受け付けの入れ子にはなりません。それでも `CloseAllFilesAndQuitVectorworks` という SDK 呼び出しの途中なので、
    受け付けの入れ子と同じく道具に SDK を呼ばせません（確認の最中に読めると確かめてあるのは
    `GetCurrentLayer()`・`GetOpenFilesList()` などに限られ、閉じようとしている図面を辿る読み方は測っていない）。
- **undo の記録が開いているかは見ません。** 刻みの中から記録が開いて見えた実測は、どれもプラグイン
  自身の呼び出しが開いて閉じずに戻した記録でした（Findings の 6 はプローブの `DeleteObject` が開いた
  記録、[Undo](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Undo.md) の PIO・`CreateLocus`・スクリプトエンジンもプローブが呼んだもの）。
  Vectorworks の GUI の操作は、刻みが入らない区間で記録を開いて閉じます（ドラッグ中・レンダリング中の
  3976 刻みで開いていたのは 0 件。同 Findings の 7）。このプラグインの書く道具は自分で記録を開き、
  戻る前に閉じる（[ブリッジ「道具の表」](bridge.md#道具の表)）ので、記録が混ざることはありません。
  ほかのプラグインが閉じ忘れた記録には備えません（CLI を使っている間は GUI を操作しない前提）。

## 拡張機能を登録しない

**メニューもパレットも PIO も登録しません。** 操作はすべて CLI から行い、更新も CLI が行う
（[インストール](install-and-update.md#インストールと更新vw2026-install)）ので、利用者の入口は要りません。

- **拡張機能を 1 つも登録しなくても、Vectorworks は読み込み、`plugin_module_main` を呼びます**
  （実機で確かめた。VW 2026。macOS も Windows も、起動の直後から `vw2026 status` が
  `running:true` になり `call ping` が応えた。Windows はタイマーの前にモジュールを留めた版で）。
- **最初の起動で利用者の許可が要ることがあります（mac も Windows も）。** Vectorworks は開発元の
  分からないプラグインを既定では読み込まず、有効化の記録が無いときは、起動の途中で
  「開発元不明のプラグイン」のダイアログを出します。そこで `cli` を「有効化」すると、次の起動から
  読み込まれます。「有効化」はモジュールのフルパスで覚えるので、`install` が同じパスへ付け替える
  更新では許可は要りません。ただし記録はダイアログを閉じるたびにそのとき居たプラグインだけで
  書き直されるので、`cli` が居ない状態で Vectorworks を起動してダイアログを閉じると、同じパスでも
  もう一度要ります
  （[SDK の調査 0](../sdk-research.md#0-拡張機能を-1-つも登録しないプラグインの読み込み分かった)）。
- **全部のプラグインが有効でも、ダイアログは約 24 時間おきに出ます。プラグインと CLI は、そのダイアログを
  勝手に閉じず、日時の書き換えで出なくすることもしません。** 呼ぶ側がダイアログのコマンド（未実装。
  [SDK リファレンス #302](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/302)）で押すことはできますが、「無効にする」を押すと次の起動から `cli` 自身が外れうるので、
  押すボタンは呼ぶ側が選びます。ダイアログの最中は
  Vectorworks が起動を終えておらず、UI からは図面を扱えません。それでもプラグインは読み込み済みで、
  刻みから図面を開く・新しい図面を作る・書く（undo の記録の中で）・保存する・閉じるが通ることを
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
scripts/                                   … 整形と検査（format.sh・lint.sh・powershell.ps1）・CI（fetch-vw-sdk.sh・vw-sdk-cache-key.sh・dev-tag.sh）・get-vw（.sh / .ps1。初回のスクリプトの雛形。リリースが get-vw2026・get-vw2025 を作る。CLI を取って install を走らせるだけ）・hooks/（cli のフック。PATH。zip の直下に入る）
resources/                                 … cli.vwr（拡張機能を登録しないので最小限。Strings/cli.vwstrings に使わない文字列 1 つだけ）
tests/                                     … 無 SDK の単体テスト・ファジングの入口・スクリプトのテスト
protocol/fixtures/                         … 通信規則の見本（C++ と Go の両方のテストが読む。docs/fixtures.md）
cli/                                       … vw2026・vw2025（Go。版はビルドタグで決め、internal/edition が持つ）。インストールと更新（install）・アンインストール（uninstall）・既定のレジストリ（internal/registry/default-vw2026.json・default-vw2025.json）もここ
```

- 名前空間は `VwCli`（`VwCli::core` / `VwCli::tools`）。図面にも配布物にも現れない内部の綴りで、
  改名しても据え置きます。
- スプールの場所・ロックは `core/Bridge` が持ちます（ロックは OS の
  呼び出しだけで SDK に依らない。OS ごとの呼び出しは `core/Files*`）。
