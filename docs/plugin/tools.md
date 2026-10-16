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
| `quit` | app | なし | `{"quitting":true}`。無題の図面があれば `internal` で失敗し、`detail` に一覧（下記） |

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

CLI はプログラムやスクリプトから使うので、**保存の確認を出さずに済む部分は、先に断ります**。ただし
**保存済みの図面の未保存の変更は SDK から読めない**ので、事前に断れるのは無題の図面だけです
（[Findings「未保存の変更ありは読めない」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#未保存の変更ありは読めない)。
`outSaved` も `fIsInMemoryOnly` も「ディスクに保存されたことがあるか」を表し、保存済みの図面に保存の確認が
出る状態でも値は変わらない）。

- **無題の図面（一度も保存していない図面）が 1 枚でもあれば、終了を頼まずに `internal` で失敗します。**
  無題の図面は、何も変更していなくても終了で保存の確認が出ます（「閉じる」で実測。終了でも同じと見る【推定】）。
  `GetOpenFilesList` の `fIsInMemoryOnly` を、受け付けの中（刻みの中から呼んでよい）で全図面について読みます。
  `error` は「無題の図面があるので終了しませんでした」を含む固定の文言で、`code` は通信規則の共通の種別の
  まま（道具ごとの種別は足さない。[通信規則「応答」](../protocol.md#応答idresjson)）です。
- **失敗の応答の `detail`** に、無題の図面の一覧を載せます（[通信規則「応答」](../protocol.md#応答idresjson)の
  `detail`。呼ぶ側が保存すべき図面を知るため）。

  ```json
  {"ok":false,"code":"internal","error":"無題の図面があるので終了しませんでした","detail":{"reason":"untitled_documents","documents":[{"name":"無題 1"}]}}
  ```

  | フィールド | 値 |
  | --- | --- |
  | `reason` | `untitled_documents`（`quit` の失敗の理由を表す固定の綴り。呼ぶ側はこれで分岐する） |
  | `documents[].name` | 図面の名前 |

  **この一覧に、保存済みの図面の未保存の変更は含まれません**（読めないため）。
- **保存済みの図面に未保存の変更があれば、保存の確認が出ます。** 事前に知る手段が無く、出ないようにも
  できません。第 1 引数は常に真のまま、応答を書いてから、受け付けの外で
  `gSDK->CloseAllFilesAndQuitVectorworks(true, false)` を頼みます（[構成「受け付けの流れ」](architecture.md#受け付けの流れ)。
  **利用者の図面を保存せずに閉じる手段は持たない**）。利用者が取り消せば Vectorworks は終わらず、呼び出しは
  戻って図面は残ります（実測。[Findings「呼んだ後の振る舞い」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#呼んだ後の振る舞いmac--windows-で実測)）。
  確認なしで終わるのは、未保存の変更が無いときです。応答は終了を頼む前に書くので、取り消されたことは
  応答では分からず、呼ぶ側が止まるのを待って気付きます（下記）。
- **CLI が開いた図面は、保存か破棄の道具で確認の種を消してから `quit` を呼びます**（下記
  「[保存確認を出さないための道具](#保存確認を出さないための道具予定)」）。利用者が GUI で操作していない限り、
  `quit` は確認なしで終わります。
- **占有の中でだけ呼べます**（[通信規則「占有」](../protocol.md#占有セッション)。占有していなければ
  `no_session`、ほかが占有していれば `occupied` で断られる）。Vectorworks が終われば占有も消えます。
- **終了の依頼の最中は断られます**（`busy`。[通信規則「Vectorworks が処理中」](../protocol.md#vectorworks-が処理中)）。
  保存の確認を開いている間に届いた 2 つ目の `quit` も `busy` で断られるので、終了の依頼は重なりません
  （この判定を残すかは段 5 の前に決める。[構成「Vectorworks が処理中」](architecture.md#vectorworks-が処理中)）。
- **再起動の手段は持ちません**（第 2 引数は常に偽）。起動し直すのは呼ぶ側で、`session start` で占有を
  取ってから `quit`（`--session` を付ける）・止まるのを待つ・`launch`・動き出すのを待つ、を組み合わせます（更新を
  挟む形を含めて[インストール「流れ」](install-and-update.md#流れ)）。
- 終了を見届けるのは呼ぶ側の役割です（`vw2026 status` を止まっていると答えるまで繰り返す。保存の確認を開いている間の扱いは
  [通信規則「生存の判定」](../protocol.md#生存の判定)）。上限を過ぎても止まらないのは、保存の確認が取り消された
  か、開いたままのときです。

### 保存確認を出さないための道具（予定）

**CLI から操作した図面については、次の道具を用意して確認の種を消せるようにします**（検討中。実装の段と
引数は決めていない）。

| 道具 | 種類 | 内容 |
| --- | --- | --- |
| 保存 | write | CLI が開いた図面を、その図面自身のパスへ上書き保存する（`SwitchToOpenFile` → `SaveActiveDocumentPath`。同じパスへの上書き保存は通り、保存で「変更あり」が消えることは実測済み） |
| 破棄して閉じる | app | CLI が開いた図面を、変更があっても保存せずに閉じる |

- **対象は、プラグインが自分で開いた（または作った）図面だけです。** 利用者が開いた図面には使えず、
  `invalid_args` で断ります。利用者の図面を保存せずに閉じる手段を持たない、という規則を保つためです。
  **この規則（CLAUDE.md の 5）は「利用者の図面」と書いていて、CLI が開いた図面の破棄は含まない解釈になる
  ので、実装の前に文言を確かめます。**
- 無題の図面には保存先が無く、上書き保存できません。CLI が図面を開くときは、保存先のパスを持つ図面
  として開きます（開く道具は[設計「決めたこと」](../design.md#決めたこと)の「対象の図面」。未実装）。
- **図面に書く道具は undo の記録の中で書きます。** undo の記録の外の書き込みは「変更あり」にならず、
  破棄するときも保存の確認なしに黙って捨てられます（Findings の同節）。書いた内容が残るかは、
  undo の手順に従っているかで変わります。

## 他のプラグインの機能

手段は `CallPluginLibrary` です（[設計「他のプラグインの道具」](../design.md#他のプラグインの道具)）。
道具の形は[未決 3](open-questions.md#3-ほかのプラグインの機能の呼び出しの形)で決めます。
