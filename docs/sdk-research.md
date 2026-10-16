# SDK の調査

**SDK の挙動が分からないまま実装に入らない**のがこのリポジトリの決めごとです。調べるのは
[SDK リファレンス](https://github.com/min-nano/vectorworks-developer-sdk-reference)の側で、
issue を立てて `Findings/` に反映されてから、それを根拠に実装します。待つ間は SDK に依らない
作業（通信規則・CLI・SDK に依らない共通部）を進めます。

## 0. 拡張機能を 1 つも登録しないプラグインの読み込み

このプラグインはメニューも PIO も登録しません（[構成](plugin/architecture.md#拡張機能を登録しない)）。
その形でも Vectorworks が読み込み、`plugin_module_main` を呼ぶか（＝OS タイマーを開始できるか）は
SDK リファレンスに記載がありません。読み込みは登録の種類に依らない見込みが高いので、
**段 2（骨格と配布）の実機確認で確かめ**、結果を SDK リファレンスの `Findings/`（Plug-in Modules）へ
足します。読み込まれなければ、目に触れないメニューを 1 つ登録する形に切り替えます。

## 1. ほかのプラグインの機能を名前で呼び出す手段（分かった）

[設計「他のプラグインの道具」](design.md#他のプラグインの道具)のため。
[SDK リファレンス #217](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/217)
で調べ、[Findings「Plug-in Interop」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Plug-in%20Interop.md) にまとまりました。

- **`ISDK::CallPluginLibrary(routineName, PluginLibraryArgTable*, status)` で呼べる**（実機確認済み）。
  名前は相手が登録した**プラグインライブラリルーチンの名前**で、ユニバーサル名でもファイル名でもない。
  相手が `kVLIBScopeUniversal` か `kVLIBScopeSDKOnly` で登録していれば届く。
- 引数は最大 11 本と結果 1 つ。文字列・数値・真偽・出力（VAR）引数が往復する。
- **相手が居なければ `false` を返し、落ちない。** 事前に有無を確かめる口は無い（`HasPlugin` は
  VectorScript だけで、ワークスペースに在るかしか見ない。`ExternalNameToID` は在る名前でも `-1`）。
- 読み込み順は気にしなくてよい。**OS タイマーの刻みの中からも呼べる。**
- メニューコマンドは `ISDK::DoMenuName` で内部名から起動できるが、引数も結果も渡せない。
  C の ABI（`dlsym` / `GetProcAddress`）は勧められていない。

残っているのは Windows での挙動（未測定）と、ハンドル・2 次元配列の引数（【ヘッダ根拠】）です。
これを踏まえた呼び出しの形は[未決 3](plugin/open-questions.md#3-ほかのプラグインの機能の呼び出しの形)で決めます。

## 2. 汎用の実行口（のちの検討）

[設計「進め方」](design.md#進め方)の 4（書く道具）を汎用にするかの判断材料。

- プラグインから Vectorworks 内蔵の Python／VectorScript を、文字列で渡して実行できるか。
  結果や出力を受け取れるか。
- メニューコマンドを名前で起動できるか → `ISDK::DoMenuName` で内部名から起動できる（[Findings「Plug-in Interop」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Plug-in%20Interop.md) の 2）。
  スクリプトエンジンでの実行も、同 3 と [Findings「Undo」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Undo.md)「間接経路」に実測がある。
- Vectorworks 自身の外部からの受け口（macOS の Apple Events、起動引数でのスクリプト実行など）が
  あるか。

## 3. 開いている図面の一覧と、前面の図面の切り替え（ほぼ分かった）

[設計「決めたこと」](design.md#決めたこと)の「対象の図面」のため。[Findings「Documents」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md) にまとまっています
（実測はメニューコマンドの中から。VW 2026 / macOS）。

| 問い | 答え |
| --- | --- |
| 開いている図面の列挙 | `GetOpenFilesList` で、文書ごとにパス・`fileRef`・前面か・未保存（`fIsInMemoryOnly`）かが取れる。`fileRef` は連番ではない不透明な鍵 |
| 前面の切り替え | `SwitchToOpenFile(fileRef)` で切り替えられ、カレントの文書とレイヤは即座に移る |
| 前面にしないまま読む | 前の文書のハンドルは、別の文書が前面でも読める（確かめたのは名前の読み書き）。描くときは前面にしてからが安全 |
| 閉じている図面を開く | `OpenDocumentPath(path, false)` で開ける。既に開いているファイルは `false`。`.sta` を渡すと無題の新規文書になる |
| undo の記録 | 開く・切り替えるでは記録は開かない。`IsCurrentlyBuildingAnUndoEvent()` は文書ごとの状態 |

- **戻り値を信じない。** `CloseDocument()` などは成功しても `false` を返す。成否は
  `GetOpenFilesList` の件数や読み戻した値で確かめる。
- **`CloseDocument()` は保存の確認を出さずに未保存の変更を捨てる。** 道具には使わない
  （[設計「本番で常駐させる前提」](design.md#本番で常駐させる前提安全)）。

残っていること:

- **OS タイマーの刻みの中から**、これらを呼んでよいか（実測はメニューコマンドの中だけ）。
- Windows での挙動。

## 4. 最初の道具が使う SDK 呼び出し

[最初に提供する道具](plugin/tools.md)のため。プラグインを 1 から書くので、ほかのプラグインで
動いていたことは根拠にしません。SDK リファレンスの `Findings/` に無いものを確かめます。

| 道具 | 呼び出し | リファレンス |
| --- | --- | --- |
| `layers` | デザインレイヤの列挙（`GetDrawingHeaderFristMember`＋`NextObject`） | [Layers and Stories「デザインレイヤの並びと並べ替え」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Layers%20and%20Stories.md)にある。シートレイヤを含むか・縮尺の読み方は未確認 |
| `classes` | クラスの一覧 | 無い |
| `layer_objects` / `object_counts` | レイヤの中のオブジェクトの列挙・種別・名前・クラス・外形 | 種別（`GetObjectTypeN`）・外形（`GetObjectBounds`）は各ページに使い方がある。列挙の順と、グループ・シンボルの中を数えるかは未確認 |
| `ping` | `GetCurrentLayer()` で図面が開いているかを判定する | **分かった**（[Timers and Notifications](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Timers%20and%20Notifications.md) の 4 の 8。使い方は[道具「ping」](plugin/tools.md#ping)） |
| `quit`（未保存の判定） | 保存済みの図面の**変更あり**を読む | **分かった: 読めない**（[#240](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/240)。[Findings「未保存の変更ありは読めない」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#未保存の変更ありは読めない)）。`outSaved`・`fIsInMemoryOnly` は「ディスクにあるか」だけを表す。そのため `quit` は図面が 0 枚のときだけ受け付ける設計にした（[道具「quit」](plugin/tools.md#quit)） |
| `quit` | OS タイマーの刻みから `CloseAllFilesAndQuitVectorworks(true, false)` を呼ぶ | 無い（Documents が「プラグインの読み込み中に呼ぶと落ちる」として Plug-in Modules を指すが、その節は見当たらない）。保存の確認を取り消したときに戻ることも未確認（[#230](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/230)。その 4 で、保存の確認の中から 2 つ目を呼んだときを確かめ、終了の依頼の最中の判定を残すかを決める。Vectorworks のダイアログの最中に呼んだときは [#235](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/235)） |
