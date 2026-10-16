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
| `quit` | app | なし | `{"quitting":true}`。未保存の図面があれば `internal` で失敗し、`detail` に一覧（下記） |

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

**CLI はプログラムやスクリプトから使うので、ダイアログを出さずに済むように設計します。**
開いている図面を先に調べ、**未保存の変更がある図面が 1 枚でもあれば、終了を頼まずに失敗で応えます**。
無いときだけ、応答を書いてから、受け付けの外で `gSDK->CloseAllFilesAndQuitVectorworks(true, false)` を
頼みます（[構成「受け付けの流れ」](architecture.md#受け付けの流れ)）。

- **未保存の変更の確認は、道具の中（受け付けの中）で `GetOpenFilesList` を読んで行います。**
  全図面を調べ、未保存の変更を持つものがあれば `internal` で失敗します。`error` は
  「未保存の変更がある図面があるので終了しませんでした」を含む固定の文言にします（`code` は
  通信規則の共通の種別のままで、道具ごとの種別は足さない。[通信規則「応答」](../protocol.md#応答idresjson)）。
  呼ぶ側は `detail` で原因を機械的に読めます。
- **失敗の応答の `detail`** に、未保存の図面の一覧を載せます（[通信規則「応答」](../protocol.md#応答idresjson)の
  `detail`。呼ぶ側が保存すべき図面を知るため）。

  ```json
  {"ok":false,"code":"internal","error":"未保存の変更がある図面があるので終了しませんでした","detail":{"reason":"unsaved_documents","documents":[{"name":"plan.vwx","path":"/Users/me/plan.vwx","untitled":false},{"name":"無題 1","path":null,"untitled":true}]}}
  ```

  | フィールド | 値 |
  | --- | --- |
  | `reason` | `unsaved_documents`（`quit` の失敗の理由を表す固定の綴り。呼ぶ側はこれで分岐する） |
  | `documents[].name` | 図面の名前 |
  | `documents[].path` | 保存先のパス。一度も保存していない図面は `null` |
  | `documents[].untitled` | 一度も保存していない新規の図面か |

  新規の図面（一度も保存していない）も未保存として数えます。
- **未保存の変更をどう判定するかは SDK リファレンスで確かめてから実装します**（CLAUDE.md の 7。
  [SDK の調査 4](../sdk-research.md#4-最初の道具が使う-sdk-呼び出し)・[SDK リファレンス #240](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/240)
  で調査中）。`GetOpenFilesList` の `fIsInMemoryOnly` が分かっているのは「保存されていない新規の図面」で、
  保存済みの図面を変更した状態（変更あり）を表すかは分かっていない。変更ありを取れなければ、
  この事前確認は新規の図面しか見つけられず、設計を見直す。
- **第 1 引数は常に真のままです**（**利用者の図面を保存せずに閉じる手段は持たない**）。事前確認のあとに
  利用者が図面を変えるなどして、確認と呼び出しの間に未保存の変更ができたときは、保存の確認が
  出ます（取り消せば Vectorworks は終わりません。この競合のときだけ。ダイアログを出さない保証ではなく、
  通常は出さない設計です）。
- **占有の中でだけ呼べます**（[通信規則「占有」](../protocol.md#占有セッション)。占有していなければ
  `no_session`、ほかが占有していれば `occupied` で断られる）。Vectorworks が終われば占有も消えます。
- **終了の依頼の最中は断られます**（`busy`。[通信規則「Vectorworks が処理中」](../protocol.md#vectorworks-が処理中)）。
  上の競合で保存の確認が出ている間に届いた 2 つ目の `quit` も `busy` で断られるので、終了の依頼は
  重なりません（この判定を残すかは段 5 の前に決める。[構成「Vectorworks が処理中」](architecture.md#vectorworks-が処理中)）。
- **再起動の手段は持ちません**（第 2 引数は常に偽）。起動し直すのは呼ぶ側で、`session start` で占有を
  取ってから `quit`（`--session` を付ける）・止まるのを待つ・`launch`・動き出すのを待つ、を組み合わせます（更新を
  挟む形を含めて[インストール「流れ」](install-and-update.md#流れ)）。
- 終了を見届けるのは呼ぶ側の役割です（`vw2026 status` を止まっていると答えるまで繰り返す。競合で保存の確認を開いている間の扱いは
  [通信規則「生存の判定」](../protocol.md#生存の判定)）。
- **未保存の図面がある場合は、呼ぶ側が保存してから呼び直します。** `quit` に保存する・捨てる指定は
  持たず、CLI にも足しません（CLI はプリミティブに保つ。[設計](../design.md#cli-はプリミティブに保つ)）。

## 他のプラグインの機能

手段は `CallPluginLibrary` です（[設計「他のプラグインの道具」](../design.md#他のプラグインの道具)）。
道具の形は[未決 3](open-questions.md#3-ほかのプラグインの機能の呼び出しの形)で決めます。
