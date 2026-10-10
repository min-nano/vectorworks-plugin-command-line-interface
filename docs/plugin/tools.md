# 最初に提供する道具

図面を**読むだけ**の道具と、Vectorworks を終わらせる 1 つ（`quit`）から始めます。更新は道具ではなく CLI の
`vw2026 install` です（[インストール](install-and-update.md#インストールと更新vw2026-install)）。どれも元のプラグインの
`vw_*` で実機で通った SDK 呼び出しだけを使います（元:`src/draw/McpBridge.cpp`）。**新しい SDK
呼び出しを使う道具は、SDK リファレンスで確かめてから足します。**

| 道具 | 種類 | 元 | 引数 | 結果 |
| --- | --- | --- | --- | --- |
| `tools` | （予約） | `vw_tools` | なし | [ブリッジ「道具の表」](bridge.md#道具の表) |
| `ping` | read | `vw_ping` | なし | 下記 |
| `layers` | read | `vw_layers` | `include_sheets`（既定 true） | `{"layers":[{"name","kind":"design"\|"sheet","scale","objects","current"}],"count"}` |
| `classes` | read | `vw_classes` | なし | `{"classes":[名前…],"count"}` |
| `layer_objects` | read | `vw_layer_objects` | `layer`（必須）・`limit`（既定 50）・`offset`・`type` | `{"layer","objects":[{"index","type","type_name","name","class","bounds":{"left","right","top","bottom"}}],"returned","total"}` |
| `object_counts` | read | `vw_object_counts` | `layer`（省略＝図面全体） | `{"layer"?,"types":[{"type","type_name","count"}],"total"}`（種別番号の昇順） |
| `quit` | app | `vw_restart` | なし | `{"quitting":true}` |

## `ping`

```json
{"version":"abc1234","branch":"main","protocol":5,"pid":4242,"session":false,"document_open":true,"current_layer":"1F"}
```

| フィールド | 値 |
| --- | --- |
| `version` | `VW_BUILD_VERSION`（短い sha。更新が効いたかを確かめる手掛かり） |
| `branch` | `VW_BUILD_BRANCH`（PR のプレリリースを入れているかが分かる） |
| `protocol` | 作法の版（[作法「版」](../protocol.md#版)） |
| `pid` | 受け付けている Vectorworks のプロセス ID（`getpid` / `GetCurrentProcessId`。2 つ起動しているときにどちらが受け付けているかが分かる） |
| `session` | いずれかの呼ぶ側が占有しているか（`session.lock` が掴まれているか。[作法「占有」](../protocol.md#占有sessionlock)）。`ping` は占有によらず誰にでも答える |
| `document_open` | `gSDK->GetCurrentLayer()` が取れるか（元と同じ） |
| `current_layer` | 現在のレイヤの名前 |

## `quit`

応答を書いてから、受け付けの外で `gSDK->CloseAllFilesAndQuitVectorworks(true, false)` を頼みます
（[構成「受け付けの流れ」](architecture.md#受け付けの流れ)）。

- **保存の確認は必ず出します**（第 1 引数は常に真。**利用者の図面を保存せずに閉じる手段は持たない**）。
  利用者が取り消せば Vectorworks は終わりません。
- **再起動の手段は持ちません**（第 2 引数は常に偽）。起動し直すのは呼ぶ側で、`quit`・`wait --down`・
  `launch`・`wait` を組み合わせます（更新を挟む形を含めて[インストール「流れ」](install-and-update.md#流れ)）。
- 終了を見届けるのは呼ぶ側の役割です（`vw2026 wait --down`。保存の確認を開いている間の扱いは
  [作法「生存の判定」](../protocol.md#生存の判定)）。

## 他のプラグインの機能

[SDK リファレンス #217](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/217)
の結果が出てから、ユニバーサル名で呼ぶ道具（例: `call` に `universal_name` と引数を渡す、
または `<提供者>.<名前>` の道具を表に動的に並べる）を設計します。
