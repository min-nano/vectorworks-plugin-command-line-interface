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
| OS タイマー（250 ms ごと） | 処理中の判定 → 受け付け 1 回（`core::Server::serve`）→ `quit` が頼まれていたら終了を頼む |

OS タイマーは起動時に開始し、最初の刻みから受け付けます。

## 受け付けの流れ

```
OS タイマー
  ├─ gSDK が無い → 何もしない
  ├─ 処理中の判定: 終了の依頼の最中 → Quitting、undo の記録が開いている → UndoOpen、ほかは Idle
  └─ core::Server::serve（受け付け 1 回。処理中の判定を渡す）
        ├─ 受け付けの入れ子（serve の途中にまた呼ばれた）→ 取り出せる要求をすべて busy で断って戻る
        ├─ ロックを掴んでいない → 取ってみる。取れなければ何もしない（次の回に取り直す）
        │                         取れたら前の回の残骸を消す
        ├─ 要求を名前の昇順で読んで消す（回の始めに並べたものだけ。読めない・消せないものは飛ばす）
        ├─ 呼ぶ側の待つ印（<id>.wait）を掴めたら、待つ者が居ないので実行しない
        ├─ 種類 write・app と session_end は占有に照らす（tools・種類 read は誰にでも）
        ├─ 種類 write・app は、処理中（UndoOpen・Quitting）なら busy で断る
        └─ 道具を実行して応答を書く（quit も応答を書いてから、終了の依頼を返す）
     quit が頼まれていたら、終了の依頼の最中の印を立てて CloseAllFilesAndQuitVectorworks。戻ったら下ろす
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

## 受け付けを止めない

**利用者が受け付けを止める手段は持ちません**（止めたければアンインストールする）。要求を受け付けの
外に溜め込むこともしません。Vectorworks の処理の妨げになる要求は、次の「Vectorworks が処理中」で
すぐに断ります。

- 何も要求が無いときの 1 回の費用は、スプールの一覧を読むことと、undo の記録が開いているかを
  問うことだけです。
- 起動の直後も最初の刻みから受け付けます。待つ時間（以前の「開始から 10 秒」）は根拠が無いので
  持ちません。起動画面の頃の刻みから `GetCurrentLayer()` と `GetOpenFilesList()` を呼んで落ちない
  ことは確かめてあります（[Findings「Timers and Notifications」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Timers%20and%20Notifications.md) の 4 の 8）。
  同じ頃の `IsCurrentlyBuildingAnUndoEvent()` は [SDK リファレンス #236](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/236) で確かめます。
- Vectorworks が刻みを配らない間（重い処理・一部のダイアログの最中）は応えません。そのとき要求は
  スプールに残り、刻みが戻れば処理されます（呼ぶ側の待ち時間の内なら応答が届く。過ぎた・呼ぶ側が
  強制終了された要求は、待つ印を掴める（または無い）ので実行しない。[通信規則「待つ印」](../protocol.md#待つ印idwait)）。
  その間もロックは掴んだままです（呼ぶ側から見た扱いは[通信規則「生存の判定」](../protocol.md#生存の判定)）。

## Vectorworks が処理中

次の間は、要求を `busy` で断ります（呼ぶ側から見た扱いは[通信規則「Vectorworks が処理中」](../protocol.md#vectorworks-が処理中)）。
判定は `Clock` が回ごとに行い、`core::Server::serve` に渡します（`core::Activity`）。入れ子の判定だけは
`core::Server` が自分で持ちます。

| 場面 | 判定 | 断る要求 | 理由 |
| --- | --- | --- | --- |
| undo の記録が開いている | `Clock` が回の始めに `gSDK->IsCurrentlyBuildingAnUndoEvent()` を 1 度問う（`UndoOpen`） | 種類 `write`・`app` | 書くと開いている記録に混ざる。読むのは安全（[Findings「Timers and Notifications」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Timers%20and%20Notifications.md) の 6） |
| 終了の依頼の最中 | `Clock` が `CloseAllFilesAndQuitVectorworks` の前後で印を立てて下ろす（`Quitting`）。保存の確認の間も刻むので、その刻みで渡す | 種類 `write`・`app` | 保存しようとしている図面を書き換えない。終了の依頼を重ねない |
| 受け付けの入れ子 | `core::Server` が `serve` の途中かを持つ | すべて | 外の回の状態（並べた要求・占有の印）を取り合わない。道具の SDK 呼び出しの途中で SDK を呼ばない |

- **undo の記録が開いていても、読む道具は応えます。** 記録が開いていることは「利用者が操作中」を
  意味しません。SDK の内部（PIO の再計算・ビューポートの更新・スクリプトエンジンなど）も開き、
  スクリプトエンジンはコマンドをまたいで開いたままにします（同 Findings の 6「`yes` の意味が広い」）。
  回ごと見送ると、残った記録 1 つで `ping` まで応えなくなるためです。残った記録がいつ閉じるかは
  [SDK リファレンス #234](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/234) で確かめます。
- 利用者がドラッグやレンダリングをしている最中にも刻みは届きますが、そのとき VW は undo の記録を
  開いていないので（同 Findings の 7）、書く道具も断られません。人の操作と書き込みの順序の扱いは
  [未決 1](open-questions.md#1-図面に書く道具の振る舞い) で決めます。
- VW のモーダルダイアログの最中に記録が開いているかはダイアログに依る見込みで、
  [SDK リファレンス #235](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/235) で確かめます
  （Findings の 6 で `yes` だったのは、記録を開かせてからダイアログを出した場面）。
- **受け付けの入れ子**は、道具の SDK 呼び出しが中でイベントを回したとき（モーダルダイアログ・進捗の
  `DoYield` など）に、そのループの中で次の刻みが届いて起きます（同 Findings の 4）。入れ子の回は
  要求を解析せずに断ります。外の回がすでに並べていた要求も断り、外の回はそれを取り出せずに飛ばします。
  いまの道具の呼び出しでイベントを回すものは知られていません。
- 終了の依頼は `serve` の外（タイマーが `serve` から戻ったあと）で行うので、保存の確認の間の刻みは
  入れ子になりません。その刻みは `Quitting` を渡してふつうに受け付け、読む道具には応えます。
  2 つ目の `quit` は `busy` で断られるので、終了の依頼は重なりません。

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
├─ Clock.{h,cpp}                           … OS タイマー・処理中の判定・終了の依頼
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
