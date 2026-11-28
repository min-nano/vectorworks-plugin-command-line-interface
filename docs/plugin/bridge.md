# 受け付け（ブリッジ）

受け渡しの形は[通信規則](../protocol.md)が真実で、ここはプラグイン側の作りです。各ファイルの
細かい手順は、そのファイルの先頭と関数のコメントにあります。

## 分け方

| ファイル | SDK | 中身 |
| --- | --- | --- |
| `src/core/Protocol.h` | 依らない | 通信規則の定数と id の検査。`protocol/fixtures/constants.json` と対（[通信規則の見本](../fixtures.md)） |
| `src/core/Json.{h,cpp}` | 依らない | 小さな JSON。読み方は Go の `encoding/json` に合わせる（[通信規則「要求」](../protocol.md#要求idreqjson)）。実数の書き出しの決まりは[道具「共通の決まり」](tools.md#共通の決まり) |
| `src/core/Files.{h,cpp}`・`FilesPosix.cpp`・`FilesWindows.cpp` | 依らない | スプールに使う OS の呼び出し（ロック・待つ印・読み書き・自分のモジュールの場所）。開き方の決まりは[通信規則「ファイル」](../protocol.md#ファイル)と[「プラグイン側の義務」](../protocol.md#プラグイン側の義務) |
| `src/core/WindowsPath.{h,cpp}` | 依らない | Windows のパスに `\\?\` を付ける変換と、UTF-8 と UTF-16 の変換。WIN32 のときだけビルドし、スプールと `save` の保存先の確かめが共に使う |
| `src/core/SaveTarget.{h,cpp}`・`SaveTargetPosix.cpp`・`SaveTargetWindows.cpp` | 依らない | `save` の保存先の確かめ（[保存先の確かめ](save-target.md)） |
| `src/core/PathCompare.h`・`PathCompareMac.cpp`・`PathCompareWindows.cpp` | 依らない | `save` の `to` と開いている図面のパスを比べる OS の呼び出し。APPLE と WIN32 のときだけビルドする（[保存先の確かめ「パスの一致の判定」](save-target.md#パスの一致の判定)） |
| `src/core/Bridge.{h,cpp}` | 依らない | スプール 1 つの操作と、モジュールのパスからスプールの場所を求める処理（[スプールの用意](#スプールの用意)） |
| `src/core/Serve.{h,cpp}` | 依らない | 受け付け 1 回の手順と占有の印（[受け付け 1 回](#受け付け-1-回coreserverserve)） |
| `src/core/Args.{h,cpp}` | 依らない | 道具の引数の読み方と、失敗の `detail` の `reason` を付ける口（[道具「共通の決まり」](tools.md#共通の決まり)） |
| `src/core/Ping`・`ReadTools`・`DocumentTools`・`Quit`（`.{h,cpp}`） | 依らない | 道具の中身の内 SDK に依らない部分（表の行 `ToolSpec`・引数の検査・確かめの順・結果の形）。SDK から読む値と SDK の呼び出しは、道具ごとの口（`Drawing`・`Documents`）で受ける |
| `src/core/Lifecycle.h` | 依らない | 起動を終えた印と終了の依頼の最中の印（`core::Lifecycle`）と、刻み 1 回の手順（`core::tick`） |
| `src/tools/ToolTable.cpp` | 依る | 道具の表 |
| `src/tools/<道具>.cpp`・`Text.h` | 依る | 道具の中身の内 SDK を呼ぶ部分（`Drawing`・`Documents` を SDK で作る）と、`TXString` を UTF-8 にする `utf8` |
| `src/Clock.cpp` | 依る | OS タイマー・通知の登録・終了の呼び出し（[構成「入口」](architecture.md#入口)） |

- **SDK を呼ぶ処理と、その前後の手順を分けます。** 手順は `src/core` に置き、SDK の呼び出しは
  引数（道具ごとの口・終了の呼び出し）で受けて、無 SDK のテストで確かめます。起動と終了の印を
  `Lifecycle.h` に置くのも同じ理由で、保存の確認が開く競合（`quit` が図面を確かめてから終了を
  頼むまでに図面が開く）と、その間に刻みが入り直すこと（Windows）は実機の確認で起こせず、
  無 SDK のテストでしか確かめられないためです。
- **OS ごとのファイルには OS の呼び出しだけを置きます。** パスを文字列として組み立てる・分ける
  処理は `Files.cpp` に 1 つだけ書き、OS ごとの違いは `Files.h` の定数と `isSeparator` の中で分けます。
- **受け付けの手順は偽の道具の表でテストします**（`tests/ServeTests.cpp`）。表せるものは
  [通信規則の見本](../fixtures.md)に置き、CLI の偽のプラグイン（`cli/internal/fakeplugin`）も
  同じ見本で確かめます。

## スプールの用意

- 場所は `<Plug-Ins>/cli/spool`（[通信規則「スプールの場所」](../protocol.md#スプールの場所)）。
  自分のモジュールの場所から求め、環境変数は読まず、**探索しません**。求められなければ、次の刻みで
  やり直します。
- `prepare`: 無ければ作ります。失敗したら次の回にやり直します（理由は受け付けの結果に載せる）。
- `lock`: `bridge.lock` を掴み、取れたら終了まで放しません（掴み方と、断った `bridge.lock` を
  1 度だけ作り直すことは[通信規則「ロック」](../protocol.md#ロックbridgelock)）。
- `sweep`: **ロックを取れた直後に 1 度だけ**、待つ印を掴める・無い id のファイルを消します
  （[通信規則「ロック」](../protocol.md#ロックbridgelock)）。

## 受け付け 1 回（`core::Server::serve`）

`serve` は道具の表・時刻と、終了の依頼の最中か（`core::Activity`。[構成「Vectorworks が処理中」](architecture.md#vectorworks-が処理中)）
と、起動の状態（`core::Startup`。道具に `ToolContext::startup` で渡す。[道具「quit」](tools.md#quit)）を
受け取ります。どちらもタイマーが `core::Lifecycle` から渡します（`core::tick`）。

0. **`serve` の途中に又呼ばれたら（受け付けの入れ子）**、外の回の状態に触れず、取り出せる要求を
   全て `busy` で断って戻ります。解析も道具の実行もしません（[通信規則「Vectorworks が処理中」](../protocol.md#vectorworks-が処理中)）。
1. ロックを掴んでいなければ用意（`prepare` → `lock` → `sweep`）をします。**ロックを取れなければ
   何もせずに戻ります**（結果は `waiting`）。
2. 回の始めに並べた要求を名前の昇順で取り出し、読めたら消し、消せたものについて待つ印を試します
   （[通信規則「待つ印」](../protocol.md#待つ印idwait)・[「プラグイン側の義務」](../protocol.md#プラグイン側の義務)）。
3. 読めたが壊れている要求には `invalid_request` で応えます（[通信規則「要求」](../protocol.md#要求idreqjson)）。
4. 要求ごとに[通信規則「占有」](../protocol.md#占有セッション)の表と「Vectorworks が処理中」に照らし、
   通れば道具を実行して応答を書きます。占有の印は `core::Server` がメモリだけに持ちます。
   道具が成功して終了の依頼（`ToolStatus::quit`）を立て、応答を書けたら、**その回の残りの要求を
   取り出さずに戻ります**（終了したら応えられないので、残りは置いたままにする）。
5. 結果（`ServeResult`。受け付けの状態 `serving` / `waiting` / `error` と、終了の依頼の有無）を
   返します。タイマーが見るのは終了の依頼だけで、終了はタイマーが `serve` から戻ってから頼みます
   （[構成「受け付けの流れ」](architecture.md#受け付けの流れ)）。

- **占有の印の発行・終了と終了の依頼は、応答を書けたときだけ反映します**（[通信規則「占有」](../protocol.md#占有セッション)）。
- **要求 1 件の処理（解析・占有・道具）の例外は `serve` が受けて `internal` で応えます**（ブリッジを
  止めない）。`serve` そのものが投げた例外は、呼ぶタイマー（`Clock`）が受け、Vectorworks へ通しません。
- 解析は要求の大きさに対して線形の時間で終えます。1 MiB の要求で受け付けの 1 回が長引き、図面の
  操作を止めないためです。

## 同じスプールを見る 2 つの Vectorworks

Windows では Vectorworks 2026 を 2 つ起動でき、どちらのプラグインも同じスプールを使います。
**先にロックを取った方が受け付け、後から来た方は毎回ロックを取りに行って失敗するだけです**（呼ぶ側から見た扱いは[通信規則「ロック」](../protocol.md#ロックbridgelock)）。

| 局面 | 起きること |
| --- | --- |
| 2 つ目の起動 | ロックを取れないので何もしない（`sweep` もしない） |
| 同時に起動 | ロックはどちらか一方だけが取れる |
| 受け付けている側がダイアログの最中など | ロックは掴んだままなので引き継がない。終われば受け付けに戻る |
| 受け付けている側が終了・異常終了 | OS がロックを放す。もう一方が次の回にロックを取り、待つ者の居ない残骸を消して引き継ぐ |
| 受け付けていない側が終了 | 何も起きない |

- 前の側が受け付けていた間に置かれて処理されなかった要求は、呼ぶ側が待っていれば残り、引き継いだ
  側が処理します。前の側が書いて呼ぶ側がまだ読んでいない応答も残ります。

## 道具の表

道具の表の 1 行は `core::Tool`（`ToolSpec` の名前・説明・`inputSchema`・種類 `ToolKind` と、中身の
`ToolFn`）です。型と各フィールドの意味は `src/core/Serve.h` にあります。

- **道具を足すときに触るのは表の 1 行と中身 1 つだけ**です。CLI は道具を知らないので直しません。
  名前・説明・`inputSchema`・種類は `src/core` の `ToolSpec`（`Ping.h`・`ReadTools.h`・`DocumentTools.h`・`Quit.h`）に置き、表は
  `core::toolOf(spec, &中身)` で組み立てます（種類を無 SDK のテストで確かめるため）。
- `tools` の結果の形と `kind` の意味は[通信規則「予約された道具」](../protocol.md#予約された道具)にあります。
  予約された道具の内 `tools`・`session_start`・`session_end` は表に載せず、`ping` と `quit` は表に載せます。
- 道具の結果は JSON のオブジェクトで返します（[通信規則「応答」](../protocol.md#応答idresjson)）。
  返す値が無ければ `Json()` でよく、`Server` が `{}` にします。
- 道具が受け付けの状態を知るのは `ToolContext` からだけです（`ping` の `session`・`quit` が見る起動の状態の
  `startup`。[道具「quit」](tools.md#quit)）。
- 引数の検査は道具の中で行います（JSON Schema の検証器は持たない）。省略できる引数には
  既定値を設け（`inputSchema` の `default` にも書く）、知らない引数・知っている引数の未知の値は
  `invalid_args` で返します（[通信規則「互換性」](../protocol.md#互換性)）。
- 道具が返す失敗の種別は `invalid_args` と `internal` だけです（分け方は[道具「共通の決まり」](tools.md#共通の決まり)）。
  他の `code` は `core::Server::serve` が付けます。呼ぶ側が機械的に読む原因は `ToolStatus::detail` に
  書きます（載せ方の決まりは[通信規則「応答」](../protocol.md#応答idresjson)）。
- **種類 `write` の道具は、undo の手順に対応するまで表に載せません。**
- **種類 `write` の道具は、自分で undo の記録を開き、受け付けから戻る前に必ず閉じます**（要求 1 件で
  undo 1 つ）。閉じずに戻ると記録が数十秒開いたままになり、その間の書き込みがそこへ入るためです
  （[Findings「Timers and Notifications」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Timers%20and%20Notifications.md) の 7「落とし穴」）。自分で閉じる限り、
  他の記録と混ざらないので、受け付けは undo の記録が開いているかを見ません
  （[構成「Vectorworks が処理中」](architecture.md#vectorworks-が処理中)）。
