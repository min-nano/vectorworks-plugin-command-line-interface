# 最初に提供する道具

図面を**読むだけ**の道具と、Vectorworks を終わらせる 1 つ（`quit`）から始めます。更新は道具ではなく CLI の
`vw2026 update` です（[更新](install-and-update.md#更新vw2026-update)）。どれも元のプラグインの
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
| `quit` | app | `vw_restart` | `restart`（既定 false） | `{"quitting":true,"restart":…}` |

## `ping`

```json
{"plugin":"cli","channel":"stable","version":"abc1234","branch":"main","protocol":1,
 "document_open":true,"current_layer":"1F"}
```

`document_open` は `gSDK->GetCurrentLayer()` が取れるかで判定します（元と同じ）。

## `quit`

応答を書いてから、受け付けの外で `gSDK->CloseAllFilesAndQuitVectorworks(true, restart)` を頼みます
（[構成「受け付けの流れ」](architecture.md#受け付けの流れ)）。

- **保存の確認は必ず出します**（第 1 引数は常に真。**利用者の図面を保存せずに閉じる手段は持たない**）。
  利用者が取り消せば Vectorworks は終わりません。
- `restart` が真なら Vectorworks 自身が起動し直します。**更新の前の終了には使いません**
  （更新は Vectorworks が動いていない間にしか入らない）。更新するときは `restart` を偽にして
  終了させ、`vw2026 update` のあとで `vw2026 launch` を呼びます（[更新](install-and-update.md#流れ)）。
- 終了・再起動を見届けるのは呼ぶ側の役割です（`vw2026 wait --down` → `vw2026 wait`）。
  保存の確認を開いている間は受け付けが見送られ、ブリッジは `unresponsive` になりますが、
  `wait --down` は `pid` のプロセスが無くなるまで「止まった」と判定しません
  （[作法「生存の判定」](../protocol.md#生存の判定)）。利用者が取り消せば `wait --down` は
  `--timeout` を過ぎて終了コード 4 で終わり、ブリッジは `live` に戻ります。

## 他のプラグインの機能

[SDK リファレンス #217](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/217)
の結果が出てから、ユニバーサル名で呼ぶ道具（例: `call` に `universal_name` と引数を渡す、
または `<提供者>.<名前>` の道具を表に動的に並べる）を設計します。
