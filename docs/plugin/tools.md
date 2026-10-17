# 最初に提供する道具

図面を**読むだけ**の道具と、Vectorworks を終わらせる 1 つ（`quit`）から始めます。最初の
リリース（[実装の順序](plan.md)の段 2）に載せるのは `ping` だけで、読む道具は段 4、`quit` は段 5 で
足します。更新は道具ではなく CLI の `vw2026 install` です（[インストール](install-and-update.md#インストールと更新vw2026-install)）。**SDK 呼び出しは、SDK リファレンスで
挙動が確かめられているものだけを使います**（無いものはリファレンスで調べてから足す。[SDK の調査](../sdk-research.md)）。

引数は、省略できるものに既定値を設けます（表の「既定」）。知らない引数と、知っている引数の
未知の値（型の違い・決まった綴り以外の値）は `invalid_args` で返します（[通信規則「互換性」](../protocol.md#互換性)）。

| 道具 | 種類 | 引数 | 結果 |
| --- | --- | --- | --- |
| `tools` | （予約） | なし | [ブリッジ「道具の表」](bridge.md#道具の表) |
| `ping` | read | なし | 下記 |
| `layers` | read | `include_sheets`（既定 true） | `{"layers":[{"name","kind":"design"\|"sheet","scale","objects","current"}],"count"}` |
| `classes` | read | なし | `{"classes":[名前…],"count"}` |
| `layer_objects` | read | `layer`（必須）・`limit`（既定 50）・`offset`（既定 0）・`type`（省略＝すべての種別） | `{"layer","objects":[{"index","type","type_name","name","class","bounds":{"left","right","top","bottom"}}],"returned","total"}` |
| `object_counts` | read | `layer`（省略＝図面全体） | `{"layer"?,"types":[{"type","type_name","count"}],"total"}`（種別番号の昇順） |
| `documents` | read | なし | [図面を扱う道具](#図面を扱う道具documentssaveclose) |
| `save` | app | `document`（必須）・`to`（省略可） | 同上 |
| `close` | app | `document`（必須） | 同上 |
| `quit` | app | なし | `{"quitting":true}`。図面が開いていれば `internal` で失敗し、`detail` に一覧（下記） |

## `ping`

```json
{"version":"abc1234","branch":"main","protocol":5,"pid":4242,"session":false,"document_open":true,"current_layer":"1F"}
```

| フィールド | 値 |
| --- | --- |
| `version` | `VW_BUILD_VERSION`（短い sha。更新が効いたかを確かめる手掛かり） |
| `branch` | `VW_BUILD_BRANCH`（PR のプレリリースを入れているかが分かる） |
| `protocol` | 通信規則の版（[通信規則「版」](../protocol.md#版)） |
| `pid` | 受け付けている Vectorworks のプロセス ID（`getpid` / `GetCurrentProcessId`。2 つ起動しているときにどちらが受け付けているかが分かる） |
| `session` | 占有の印を発行しているか（[通信規則「占有」](../protocol.md#占有セッション)）。`ping` は占有によらず誰にでも答える |
| `document_open` | 図面が開いているか（`gSDK->GetCurrentLayer()` が `nullptr` でないか） |
| `current_layer` | 現在のレイヤの名前。図面が開いていなければ `null` |

図面が 1 枚も開いていないとき、`GetCurrentLayer()` は落ちずに `nullptr` を返し、その判定は
`GetOpenFilesList()` の件数と常に一致します（起動直後も、全部閉じた後も。mac / Windows。
[Findings「Timers and Notifications」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Timers%20and%20Notifications.md) の 4 の 8）。
図面が無いときは次のように答えます。

```json
{"version":"abc1234","branch":"main","protocol":5,"pid":4242,"session":false,"document_open":false,"current_layer":null}
```

例の `protocol` は書いた時点の版です（[通信規則「版」](../protocol.md#版)）。

## `quit`

CLI はプログラムやスクリプトから使うので、**保存の確認を出さない**ことを `quit` の前提にします。
保存済みの図面の未保存の変更は SDK から読めず（[Findings「未保存の変更ありは読めない」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#未保存の変更ありは読めない)。
`outSaved` も `fIsInMemoryOnly` も「ディスクに保存されたことがあるか」を表すだけ）、無題の図面は変更が無くても
終了で保存の確認が出ます。そこで **`quit` は、図面を 1 枚も開いていないときだけ受け付けます**。
開いている図面を閉じておくのは呼ぶ側の役割です。

- **図面が 1 枚でも開いていれば、終了を頼まずに `internal` で失敗します。** 受け付けの中で
  `GetOpenFilesList` の件数を読みます（図面が 0 枚のときも落ちない。[道具「ping」](#ping)）。
  `error` は「図面が開いているので終了しませんでした。先に図面を閉じてください」を含む固定の文言で、
  `code` は通信規則の共通の種別のまま（道具ごとの種別は足さない。[通信規則「応答」](../protocol.md#応答idresjson)）です。
- **失敗の応答の `detail`** に、開いている図面の一覧を載せます（[通信規則「応答」](../protocol.md#応答idresjson)の
  `detail`。呼ぶ側が閉じるべき図面を知るため）。

  ```json
  {"ok":false,"code":"internal","error":"図面が開いているので終了しませんでした。先に図面を閉じてください","detail":{"reason":"documents_open","documents":[{"name":"plan.vwx","path":"/Users/me/plan.vwx","untitled":false},{"name":"名称未設定 1","path":"/Applications/Vectorworks 2026/名称未設定 1","untitled":true}]}}
  ```

  | フィールド | 値 |
  | --- | --- |
  | `reason` | `documents_open`（`quit` の失敗の理由を表す固定の綴り。呼ぶ側はこれで分岐する） |
  | `documents[].name` | 図面の名前 |
  | `documents[].path` | 図面のパス（`documents` と同じ。無題の図面は「アプリケーションのフォルダ＋名称未設定 N」でファイルとしては存在しない） |
  | `documents[].untitled` | 一度も保存していない図面か |
- **図面が 0 枚なら、保存の確認は出ません。** 応答を書いてから、受け付けの外で
  `gSDK->CloseAllFilesAndQuitVectorworks(true, false)` を頼みます（[構成「受け付けの流れ」](architecture.md#受け付けの流れ)）。
  第 1 引数は常に真です（**利用者の図面を保存せずに閉じる手段は持たない**）。確かめてから頼むまでの間に利用者が
  図面を開くと、保存の確認が出うる競合が残りますが、`quit` は占有の中でだけ呼べ、呼ぶ側が図面を閉じた直後に
  呼ぶ使い方なので、通常は起きません。
- **図面を保存して閉じるのは呼ぶ側です。** 保存するか捨てるかも呼ぶ側が決めます（`quit` に保存する・捨てる指定は持たず、
  CLI にも足さない。[設計](../design.md#cli-はプリミティブに保つ)）。保存（`save`）してから閉じる（`close`）
  と、[下記の道具](#図面を扱う道具documentssaveclose)で保存確認なしに閉じられます。
- **占有の中でだけ呼べます**（[通信規則「占有」](../protocol.md#占有セッション)。占有していなければ
  `no_session`、ほかが占有していれば `occupied` で断られる）。Vectorworks が終われば占有も消えます。
- **終了の依頼の最中は断られます**（`busy`。[通信規則「Vectorworks が処理中」](../protocol.md#vectorworks-が処理中)）。
  上の競合で保存の確認が開いている間に届いた 2 つ目の `quit` も `busy` で断られるので、終了の依頼は重なりません
  （この判定を残すかは段 5 の前に決める。[構成「Vectorworks が処理中」](architecture.md#vectorworks-が処理中)）。
- **再起動の手段は持ちません**（第 2 引数は常に偽）。起動し直すのは呼ぶ側で、`session start` で占有を
  取ってから `quit`（`--session` を付ける）・止まるのを待つ・`launch`・動き出すのを待つ、を組み合わせます（更新を
  挟む形を含めて[インストール「流れ」](install-and-update.md#流れ)）。
- 終了を見届けるのは呼ぶ側の役割です（`vw2026 status` を止まっていると答えるまで繰り返す。保存の確認を開いている間の扱いは
  [通信規則「生存の判定」](../protocol.md#生存の判定)）。

## 図面を扱う道具（`documents`・`save`・`close`）

`quit` の前に図面を保存して閉じるための道具です（[実装の順序](plan.md)の段 4）。使う SDK 呼び出しは SDK
リファレンスで確かめてあります（[Findings「指定した図面を保存する・閉じる」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#指定した図面を保存する閉じる前面に依存せず)。
VW 2026 / macOS。Windows は未測定）。**変更を捨てて閉じる道具は持ちません**（人の変更を黙って捨てる手段を
作らない）。保存は呼ぶ側の責任で、保存してから閉じれば保存の確認は出ません。必要のない図面は、一時フォルダなどへ
保存してから閉じ、ファイルは利用者が自分で消します。

**前面の図面（コンテキスト）に依存せず、対象の図面を引数で指定します。** プラグインが自分で開いた図面かを
覚えることもしません（どの図面にも使える）。

| 道具 | 種類 | 引数 | 結果 |
| --- | --- | --- | --- |
| `documents` | read | なし | `{"documents":[{"name","path","untitled","active"}],"count"}` |
| `save` | app | `document`（必須）・`to`（省略可） | `{"document","path"}`（保存した後の図面のパス） |
| `close` | app | `document`（必須） | `{"closed":true}` |

- **`document`** は対象の図面のパスで、`documents` の `path` を渡します。パスは図面を一意に決めます
  （同じファイル名を別のフォルダに保存した 2 枚も見分けられる）。**無題の図面のパスは「アプリケーションの
  フォルダ＋名称未設定 N」**で、N はセッション全体の連番です（開いている間は重ならない。ファイルとしては
  存在しない）。`untitled` が真なら一度も保存していない図面です。開いている図面に一致しなければ
  `invalid_args` で断ります。
- **`save`** は対象の図面を保存します。種類は `app` です（図面の中身は変えず、ファイルに書く。
  [ブリッジ「道具の表」](bridge.md#道具の表)）。`to` を省くと、その図面自身のパスへ上書き保存します（無題の図面は
  保存先が無いので `to` を要します。一時フォルダなどを指定する）。
  - **`to` を渡せるのは無題の図面だけです。** 保存済みの図面に `to` を渡すと `invalid_args` で断ります。
    別名保存は図面のパスを保存先へ移す（「名前を付けて保存」と同じ）ので、利用者の図面に許すと、利用者が
    あとで ⌘S を押したときに一時フォルダのファイルへ保存されてしまうためです。無題の図面は元のファイルが
    無く、利用者の作業を移すことにならないので許します。結果の `path` は保存した後のパスです。
  - **保存先が既にあるファイルへは保存できません**（SDK が `GSError` 50331649 を返し、何も起きない）。
    **保存先が別の図面で開いているファイルなら、SDK が警告のダイアログを出して人が OK を押すまで戻りません。**
    そこで **`to` が開いている図面のパスと一致するときは、呼ぶ前に `invalid_args` で断ります**。保存先が
    開いていない既存ファイルのときは、SDK のエラーを `internal` で返します。上書きしたければ、呼ぶ側が先に
    ファイルを消します。
  - 内部では `GetOpenFilesList` からパスの一致する項目の `fileRef` を引き、`SwitchToOpenFile` →
    `SaveActiveDocumentPath` で保存します。**前面の図面とカレントレイヤは、保存の前後で元に戻せます**
    （戻すのも `SwitchToOpenFile`）。
- **パスの一致の判定。** `document` は `documents` が返した文字列と**完全に一致**するものを探します（呼ぶ側は
  `documents` の結果をそのまま渡す）。`to` は新しいパスで、開いている図面の保存先との衝突を**見逃さない側**に倒して
  比べます: 絶対パスを要し（相対パスは `invalid_args`）、区切り文字を揃え、Unicode を正規化（NFC。macOS のファイル名は
  NFD で返ることがある）し、**大文字と小文字を区別せず**に比べます。大文字と小文字だけが違う別のファイルも断る
  ことになりますが、警告のダイアログが出るより安全です。
- **`close`** は対象の図面を閉じます。**保存はしません。** 種類は `app` です。
  対象の図面を `SwitchToOpenFile` で前面にしてから `DoMenuName("Close", 0)` を呼びます（メニューの「閉じる」と
  同じで、前面の図面に効く。`CloseDocument()` は確認なしで変更を捨てるので使いません）。
  - **保存した直後の図面は、確認なしに閉じます。** 確認が出るのは、無題の図面（無変更でも）、
    `OpenDocumentPath` で開いただけの図面（何もしなくても。開いた時点で「変更あり」になる。同じパスへ
    `save` してから閉じれば出ない）、`MarkCurrentDocChanged()` した図面です。
  - **確認が出ると、人が答えるまで呼び出しから戻りません。** その間の刻みの受け付けは入れ子になり、
    要求を `busy` で断ります（[構成「Vectorworks が処理中」](architecture.md#vectorworks-が処理中)）。
  - **戻り値（常に 0）では成否が分かりません。** 呼び出しの後に `GetOpenFilesList` を読み、対象の図面が
    残っていれば（確認がキャンセルされた）`internal` で失敗します。
  - 閉じたあとに前面になるのは別の図面です。`close` の前の前面の図面が残っていれば、`SwitchToOpenFile` で
    戻します（前面を閉じたときは戻せない）。
- **undo イベントの外で足された図形は「変更あり」にならず、`close` で確認なしに黙って捨てられます。**
  VW の仕様で、プラグインからは検出も防止もできません（「確認が出ないから変更は無い」とは読めない）。
  **この危険を認めたうえで `close` を持つ、と決めます。** 利用者の図面を保存せずに閉じる道具を持たないという
  規則（CLAUDE.md の 5）は、変更を意図して捨てる手段を作らないことを指し、`close` は捨てる手段ではありません
  （変更が分かれば保存の確認が出る）。このプラグインの図面に書く道具は undo の記録の中で書くので起きません。
  ほかのプラグインやスクリプトが記録の外で書いた変更は、呼ぶ側が `close` の前に `save` します。
- 図面を開く道具は[設計「決めたこと」](../design.md#決めたこと)の「対象の図面」にあります（未実装）。
  `OpenDocumentPath` で開いた図面は、`save` してから `close` します。

## 他のプラグインの機能

手段は `CallPluginLibrary` です（[設計「他のプラグインの道具」](../design.md#他のプラグインの道具)）。
道具の形は[未決 3](open-questions.md#3-ほかのプラグインの機能の呼び出しの形)で決めます。
