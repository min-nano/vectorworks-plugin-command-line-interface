# 最初に提供する道具

図面を**読むだけ**の道具と、殻に頼む 2 つ（更新・再起動）から始めます。どれも元のプラグインの
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
| `update` | shell | `vw_update` | `restart_if_needed`（既定 false）・`branch`（開発版だけ） | 下記 |
| `restart` | shell | `vw_restart` | なし | `{"restarting":true}` |

## `ping`

```json
{"plugin":"cli","channel":"stable","version":"abc1234","branch":"main","protocol":1,
 "document_open":true,"current_layer":"1F"}
```

`document_open` は `gSDK->GetCurrentLayer()` が取れるかで判定します（元と同じ）。

## `update`

自動アップデートの経路（`UpdaterFlow` の 1 本。[インストールと更新](install-and-update.md)）を、
**ダイアログを出さずに**通します（元:`RemoteDevUpdateWith`）。

- 安定版: 最新の `stable` が入っているものと違えば入れる。
- 開発版: `branch`（省略＝いま入っているブランチ）の最新のビルドを入れる。
- 結果: `{"outcome":"installed"|"no_new_build"|"no_such_branch"|"needs_restart"|"failed"|"check_failed",
  "previous","version","message","restart_required","restarting"}`。
  `ok` は `installed` / `no_new_build` / `needs_restart` のとき真。
- 本体だけが変わったときは本体を降ろして読み直すので、**応答は新しい本体が書きます**
  （`payload_version` で確かめられる）。殻まで変わったときは `restart_required` が真で、
  `restart_if_needed` が真なら続けて再起動します。

## `restart`

応答を書かせてから `gSDK->CloseAllFilesAndQuitVectorworks(true, true)` を頼みます
（保存の確認は出す。**利用者の図面を保存せずに閉じる手段は持たない**）。再起動を見届けるのは
呼ぶ側の役割です（`vw2026 wait --down` → `vw2026 wait`）。

## 他のプラグインの機能

[SDK リファレンス #217](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/217)
の結果が出てから、ユニバーサル名で呼ぶ道具（例: `call` に `universal_name` と引数を渡す、
または `<提供者>.<名前>` の道具を表に動的に並べる）を設計します。
