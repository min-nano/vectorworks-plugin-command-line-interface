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
| OS タイマー（250 ms ごと） | 受け付け 1 回（`core::Server::serve`）→ `quit` が頼まれていたら終了を頼む |

OS タイマーは起動時に開始し、最初の刻みから受け付けます。

## 受け付けの流れ

```
OS タイマー
  ├─ gSDK が無い → 何もしない
  └─ core::Server::serve（受け付け 1 回。終了の依頼の最中なら Quitting、ほかは Idle を渡す）
        ├─ 受け付けの入れ子（serve の途中にまた呼ばれた）→ 取り出せる要求をすべて busy で断って戻る
        ├─ ロックを掴んでいない → 取ってみる。取れなければ何もしない（次の回に取り直す）
        │                         取れたら前の回の残骸を消す
        ├─ 要求を名前の昇順で読んで消す（回の始めに並べたものだけ。読めない・消せないものは飛ばす）
        ├─ 呼ぶ側の待つ印（<id>.wait）を掴めたら、待つ者が居ないので実行しない
        ├─ 種類 write・app と session_end は占有に照らす（tools・種類 read は誰にでも）
        ├─ 種類 write・app は、終了の依頼の最中（Quitting）なら busy で断る
        └─ 道具を実行して応答を書く（quit は GetOpenFilesList で開いている図面を調べ、あれば
           detail に一覧を付けて失敗で応える。無ければ応答を書いてから、終了の依頼を返す）
     quit が頼まれていたら、終了の依頼の最中の印を立てて CloseAllFilesAndQuitVectorworks。戻ったら下ろす
```

- **SDK 呼び出しはメインスレッドからだけ**行います。受け付け用のスレッドは持ちません。
- **受け付けの中にループを書きません**（図面が操作できなくなる）。長く走る道具は持ちません
  （[通信規則「予約された道具」](../protocol.md#予約された道具)）。
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

次の間は、要求を `busy` で断ります（呼ぶ側から見た扱いは[通信規則「Vectorworks が処理中」](../protocol.md#vectorworks-が処理中)）。

| 場面 | 判定 | 断る要求 | 理由 |
| --- | --- | --- | --- |
| 終了の依頼の最中 | `Clock` が `CloseAllFilesAndQuitVectorworks` の前後で印を立てて下ろし、その間の刻みで `core::Server::serve` に `Quitting` を渡す（保存の確認の間も刻む） | 種類 `write`・`app` | 保存しようとしている図面を書き換えない。終了の依頼を重ねない |
| 受け付けの入れ子 | `core::Server` が `serve` の途中かを持つ | すべて | 外の回の状態（並べた要求・占有の印）を取り合わない。道具の SDK 呼び出しの途中で SDK を呼ばない |

- **受け付けの入れ子**は、道具の SDK 呼び出しが中でイベントを回したとき（モーダルダイアログ・進捗の
  `DoYield` など）に、そのループの中で次の刻みが届いて起きます（[Findings「Timers and Notifications」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Timers%20and%20Notifications.md) の 4）。
  入れ子の回は要求を解析せずに断ります。外の回がすでに並べていた要求も断り、外の回はそれを取り出せずに
  飛ばします。いまの道具の呼び出しでイベントを回すものは知られていません。
- **終了の依頼の最中の判定は暫定です。** 守る相手は、`quit` を頼んだ当の呼ぶ側の呼び直しだけです
  （`write`・`app` は占有の中でしか呼べないため）。保存の確認の中で 2 つ目の
  `CloseAllFilesAndQuitVectorworks` を呼んだときに何が起きるかを [SDK リファレンス #230](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/230) の 4 で確かめ、
  段 6 の前に、この判定を残すか（残すなら、入れ子の判定にまとめて保存の確認の間はすべて断るか）を決めます。
  終了の依頼は `serve` の外（タイマーが `serve` から戻ったあと）で行うので、保存の確認の間の刻みは
  入れ子にならず、いまは読む道具に応えます。
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
  （段 2 の実機確認。VW 2026。macOS も Windows も、起動の直後から `vw2026 status` が
  `running:true` になり `call ping` が応えた。Windows はタイマーの前にモジュールを留めた版で）。
- **最初の起動で利用者の許可が要ることがあります（mac も Windows も）。** Vectorworks は開発元の
  分からないプラグインを既定では読み込まず、そのパスで初めて読み込むときに、起動の途中で
  「開発元不明のプラグイン」のダイアログを出します。そこで `cli` を「有効化」すると、次の起動から
  読み込まれます。「有効化」はモジュールのフルパスで覚えるので、以前有効にした `<Plug-Ins>/cli` へ
  入れ直すときと、`install` が同じパスへ付け替える更新では許可は要りません
  （[SDK の調査 0](../sdk-research.md#0-拡張機能を-1-つも登録しないプラグインの読み込み)）。
- **全部のプラグインが有効でも、ダイアログは約 24 時間おきに出ます。そのダイアログは閉じずに置き、
  日時の書き換えやボタンを押すスクリプトで出なくすることはしません。** ダイアログの最中は
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
├─ core/                                   … SDK に依らない（Protocol・Json・Files・Bridge・Serve・Ping）。プラグインとテストがリンク
└─ tools/                                  … 道具の表（ToolTable.cpp）と中身（Ping.cpp など。SDK 依存。SDK に依らない部分は core へ）
scripts/                                   … 整形と検査（format.sh・lint.sh・powershell.ps1）・CI（fetch-vw-sdk.sh・vw-sdk-cache-key.sh・dev-tag.sh）・get-vw2026（.sh / .ps1。CLI を取って install を走らせるだけ。段 3）・hooks/（cli のフック。PATH。zip の直下に入る。段 3）
resources/                                 … cli.vwr（拡張機能を登録しないので最小限。Strings/cli.vwstrings に使わない文字列 1 つだけ）
tests/                                     … 無 SDK の単体テスト・ファジングの入口・スクリプトのテスト
protocol/fixtures/                         … 通信規則の見本（C++ と Go の両方のテストが読む。docs/fixtures.md）
cli/                                       … vw2026（Go）。インストールと更新（install）・アンインストール（uninstall）・既定のレジストリ（internal/registry/default.json）もここ
```

- 名前空間は `VwCli`（`VwCli::core` / `VwCli::tools`）。図面にも配布物にも現れない内部の綴りで、
  改名しても据え置きます。
- スプールの場所・ロックは `core/Bridge` が持ちます（ロックは OS の
  呼び出しだけで SDK に依らない。OS ごとの呼び出しは `core/Files*`）。
