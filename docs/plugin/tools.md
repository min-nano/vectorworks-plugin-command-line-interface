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
| `quit` | app | なし | `{"quitting":true}` |

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
| `document_open` | `gSDK->GetCurrentLayer()` が取れるか |
| `current_layer` | 現在のレイヤの名前 |

## `quit`

応答を書いてから、受け付けの外で `gSDK->CloseAllFilesAndQuitVectorworks(true, false)` を頼みます
（[構成「受け付けの流れ」](architecture.md#受け付けの流れ)）。

- **保存の確認は必ず出します**（第 1 引数は常に真。**利用者の図面を保存せずに閉じる手段は持たない**）。
  利用者が取り消せば Vectorworks は終わりません。
- **占有の中でだけ呼べます**（[通信規則「占有」](../protocol.md#占有セッション)。占有していなければ
  `no_session`、ほかが占有していれば `occupied` で断られる）。Vectorworks が終われば占有も消えます。
- **終了の依頼の最中は断られます**（`busy`。[通信規則「Vectorworks が処理中」](../protocol.md#vectorworks-が処理中)）。
  保存の確認を開いている間に届いた 2 つ目の `quit` も `busy` で断られるので、終了の依頼は重なりません
  （この判定を残すかは段 5 の前に決める。[構成「Vectorworks が処理中」](architecture.md#vectorworks-が処理中)）。
- **再起動の手段は持ちません**（第 2 引数は常に偽）。起動し直すのは呼ぶ側で、`session start` で占有を
  取ってから `quit`（`--session` を付ける）・止まるのを待つ・`launch`・動き出すのを待つ、を組み合わせます（更新を
  挟む形を含めて[インストール「流れ」](install-and-update.md#流れ)）。
- 終了を見届けるのは呼ぶ側の役割です（`vw2026 status` を止まっていると答えるまで繰り返す。保存の確認を開いている間の扱いは
  [通信規則「生存の判定」](../protocol.md#生存の判定)）。

## 他のプラグインの機能

手段は `CallPluginLibrary` です（[設計「他のプラグインの道具」](../design.md#他のプラグインの道具)）。
道具の形は[未決 3](open-questions.md#3-ほかのプラグインの機能の呼び出しの形)で決めます。
