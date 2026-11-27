# SDK の調査

**SDK の挙動が分からないまま実装に入らない**のがこのリポジトリの決めごとです。調べるのは
[SDK リファレンス](https://github.com/min-nano/vectorworks-developer-sdk-reference)の側で、
issue を立てて `Findings/` に反映されてから、それを根拠に実装します。待つ間は SDK に依らない
作業（通信規則・CLI・SDK に依らない共通部）を進めます。

このページに置くのは、調べ終えたものの索引（結果が SDK リファレンスのどこにあり、このリポジトリのどこで使って
いるか）と、**調べないと決めたもの**です。調べた結果そのもの（挙動・実測）は `Findings/` が真実で、ここへは
写しません。待っているもの・調べ残し（測れていない・確かめていない挙動）は、このリポジトリの issue で扱います。
節の番号は他のページから参照しています（欠けた番号は issue へ移した節）。

## 0. 拡張機能を 1 つも登録しないプラグインの読み込み（分かった）

このプラグインはメニューも PIO も登録しません。その形でも読み込まれて常駐することと、そのために要る扱いが
分かりました。結果はどれも [Findings「Plug-in Modules」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Plug-in%20Modules.md)にあります。

| 調べたこと | 結果のある節（issue） | このプラグインでの扱い |
| --- | --- | --- |
| 拡張機能なしで読み込まれ、起動時から常駐するか | 「拡張機能を 1 つも登録しないモジュールも、有効なら起動時から常駐する」（[#252](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/252)） | [構成「拡張機能を登録しない」](plugin/architecture.md#拡張機能を登録しない) |
| 「開発元不明のプラグイン」のダイアログと「有効化」の覚え方・ダイアログの最中に図面を扱えるか | 「開発元不明のプラグイン」のダイアログ（[#258](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/258)）・「ダイアログの最中も、プラグインは刻みから図面を扱える」（[#264](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/264)） | 同上・[インストール「入れる先のフォルダ」](plugin/install-and-update.md#入れる先のフォルダ) |
| Windows の Vectorworks が起動時にモジュールを一度降ろして読み込み直すこと | 「【罠】Windows の VW は起動時にモジュールを一度降ろして読み込み直す」（[#253](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/253)） | タイマーを作る前にモジュールを留める（[構成「受け付けの流れ」](plugin/architecture.md#受け付けの流れ)） |
| Windows の終了時のアクセス違反（`0xC0000005`）と、新しい図面の最初の描画中に落ちたこと | 「OS タイマーを持つ常駐プラグイン（留めたモジュール）は、終了時の落ち方を変えない」（[#257](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/257)・[#269](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/269)）・「新しい図面の最初の描画」（[#270](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/270)） | 常駐するプラグインの有無で差が無いので、タイマーを止める・留め方を変えるといった作法は足さない（原因は分かっていない）。測り方は [Windows の実機での確かめ方](plugin/windows-measurements.md) |

## 1. 他のプラグインの機能を名前で呼び出す手段（分かった）

[設計「他のプラグインの道具」](design.md#他のプラグインの道具)のため。[#217](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/217) で調べ、
[Findings「Plug-in Interop」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Plug-in%20Interop.md) にまとまりました。**手段は `ISDK::CallPluginLibrary` です。**

- 採らなかった手段: メニューコマンドの起動（`ISDK::DoMenuName`。引数も結果も渡せない）・C の ABI（`dlsym` /
  `GetProcAddress`。勧められていない）。

## 3. 開いている図面の一覧と、前面の図面の切り替え（分かった）

[設計「決めたこと」](design.md#決めたこと)の「対象の図面」のため。[Findings「Documents」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md) にまとまっています
（実測はメニューコマンドの中と OS タイマーの刻みの中から。Vectorworks 2026 / mac。Windows も振る舞いは同じで、違いは
[同「Windows」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#windows振る舞いは-mac-と同じ違うのはパスの比べ方と開ける図面は-8-枚まで)。[#283](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/283)）。

| 問い | 答え |
| --- | --- |
| 開いている図面の列挙 | `GetOpenFilesList` で、文書ごとにパス・`fileRef`・前面か・未保存（`fIsInMemoryOnly`）かが取れる。`fileRef` は連番ではない不透明な鍵 |
| 前面の切り替え | `SwitchToOpenFile(fileRef)` で切り替えられ、カレントの文書とレイヤは即座に移る |
| 前面にしないまま読む | 前の文書のハンドルは、別の文書が前面でも読める（確かめたのは名前の読み書き）。描くときは前面にしてからが安全 |
| 閉じている図面を開く | `OpenDocumentPath(path, false)` で開ける。既に開いているファイルは `false`。`.sta` を渡すと無題の新規文書になる |
| undo の記録 | 開く・切り替えるでは記録は開かない。`IsCurrentlyBuildingAnUndoEvent()` は文書ごとの状態 |

- 一覧・切り替え・保存・閉じるの使い方は[道具「図面を扱う道具」](plugin/tools.md#図面を扱う道具documentssaveclose)に
  あります（どれも OS タイマーの刻みの中から呼べる。[#240](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/240)・[#243](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/243)）。
- 開いていられる図面は、Windows では 8 枚までです（mac は測っていない）。

## 4. 道具が使う SDK 呼び出し

[道具](plugin/tools.md)のため。プラグインを 1 から書くので、他のプラグインで動いていたことは根拠にしません。
どれも調べ終えていて、使い方は道具のページにあります。

| 道具 | 調べたこと | 結果のある場所 | 使い方 |
| --- | --- | --- | --- |
| `ping` | `GetCurrentLayer()` で図面が開いているかを判定する | [Timers and Notifications](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Timers%20and%20Notifications.md) の 4 の 8 | [道具「ping」](plugin/tools.md#ping) |
| `layers` | レイヤの列挙・種類・順序・現在のレイヤ・縮尺 | [#281](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/281)。[Layers and Stories「レイヤを列挙する（デザインとシート・種類・順序・現在・縮尺）」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Layers%20and%20Stories.md) | [道具「layers」](plugin/tools.md#layers) |
| `classes` | クラスの一覧（`ForEachClass`） | [#280](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/280)。[Attributes and Classes「クラスの一覧は `ForEachClass` で列挙する」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Attributes%20and%20Classes.md) | [道具「classes」](plugin/tools.md#classes) |
| `layer_objects`・`object_counts` | レイヤの中のオブジェクトの列挙・数え方・種別・名前・クラス・外形 | [#282](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/282)。[Layer Objects](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Layer%20Objects.md) | [道具「layer_objects・object_counts」](plugin/tools.md#layer_objectsobject_counts) |
| `documents`・`save`・`close` | 図面の一覧・名指した図面の新しいパスへの保存・保存の確認を出さずに閉じること（mac・Windows） | [#243](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/243)・[#283](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/283)・[#305](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/305)。[Documents「指定した図面を保存する・閉じる」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#指定した図面を保存する閉じる前面に依存せず) | [道具「図面を扱う道具」](plugin/tools.md#図面を扱う道具documentssaveclose) |
| `save` | 上書き保存がダイアログを出す場面・コピーで開く図面（古い版・読み取り専用・使用中）・「すでに開かれています」の比べ方・保存の後の取り消し | [#288](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/288)・[#291](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/291)。[Documents「利用者の図面を上書き保存する」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#利用者の図面を上書き保存するダイアログが出る場面と保存先の文字列288) | 同上（上書き保存は提供しない） |
| `save` | 保存先のパスの形（UNC・`\\?\`・相対・`.`・`..`・段の末尾の `.` と空白）・親のフォルダに書き込めないとき | [#294](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/294)・[#299](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/299)・[#304](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/304)。[Documents「保存先のパスの形」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#保存先のパスの形unc相対-と-294) | [保存先の確かめ](plugin/save-target.md) |
| `save`・`close` | 大きな図面の保存と「閉じる」が呼び出しの中で終わるか・イベントを回すか | [#290](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/290)。[Documents「大きな図面でも、呼び出しの中で終わる」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#大きな図面でも呼び出しの中で終わる290macwindows-は下の節実務の図面は測っていない) | [道具「図面を扱う道具」](plugin/tools.md#図面を扱う道具documentssaveclose)・[通信規則「予約された道具」](protocol.md#予約された道具)の「長く掛かる道具」 |
| `close`・`quit` | `CloseDocument()` で閉じた図面が中に残り、終了で保存の確認が出ること | [#321](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/321)・[#323](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/323)。[Documents「閉じた図面が残り、終了で保存の確認が出る」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#閉じた図面が残り終了で保存の確認が出る321323macwindows) | [道具「閉じた図面の片付け」](plugin/tools.md#閉じた図面の片付け) |
| `quit` | 保存済みの図面の「変更あり」を読めるか（読めない） | [#240](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/240)。[Documents「未保存の変更ありは読めない」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#未保存の変更ありは読めない) | [道具「quit」](plugin/tools.md#quit)（図面が 0 枚のときだけ受け付ける） |
| `quit` | OS タイマーの刻みから `CloseAllFilesAndQuitVectorworks(true, false)` を呼ぶこと・保存の確認の最中の刻み・戻った後の 2 度目 | [#230](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/230)・[#235](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/235)・[#313](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/313)。[Timers and Notifications](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Timers%20and%20Notifications.md) の 4 の 9 | [構成「Vectorworks が処理中」](plugin/architecture.md#vectorworks-が処理中) |
| `quit` | 起動の途中に終了を頼むとどうなるか・起動を終えた目印 | [#312](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/312)・[#317](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/317)。同 4 の 9「起動の途中の刻みから」「起動を終えた目印」 | [道具「quit」](plugin/tools.md#quit)・[構成「入口」](plugin/architecture.md#入口) |

## 5. 同じプラグインが 2 つあるときの挙動（調べていない。実装を待たせない）

同じプラグイン（同じユニバーサル名・拡張機能の UUID・リソースの識別子を登録するモジュール）が
`Plug-Ins` の 2 か所にあるとき、Vectorworks が何をもって衝突とみなし、どう振る舞うかは
SDK リファレンスの `Findings/` に記載がありません。`vw2026 install` はこれを判定せず、二重に
入れないのを利用者の責任としたので（[レジストリ「提供側に求めること」](registry.md#提供側に求めること)）、
実装はこの調査を待ちません。判定を足すことになったら、先に SDK リファレンスで issue を立てます。

## 6. プラグインのフォルダの中のモジュール以外のものと、モジュールの読み込み元（分かった）

`<Plug-Ins>/cli/` にはモジュールの他に `bin/`（CLI の実行ファイル）・`hooks/`・`build.json`・
`vw2026-install.json`・`spool/` が入ります（[インストール「CLI とスプールの置き場所」](plugin/install-and-update.md#cli-とスプールの置き場所)）。
[#277](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/277) で調べ、[Findings「Plug-in Modules」の「プラグインのフォルダの『モジュール以外のもの』は無視され、モジュールは置いた場所から読まれる」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Plug-in%20Modules.md#プラグインのフォルダのモジュール以外のものは無視されモジュールは置いた場所から読まれるwindowsmacos)に
まとまりました（Windows も mac も同じ）。このリポジトリが頼っているのは次の 3 つです。

- Vectorworks は `Plug-Ins` の下のモジュール以外のファイルとサブフォルダを無視する（スプールに前の回の残りが
  入ったまま起動しても同じ）。
- Vectorworks はモジュールを置いた場所のファイルからそのまま読み込むので、プラグインがモジュールの場所から求める
  スプールは `<Plug-Ins>/cli/spool/` になる（[通信規則「スプールの場所」](protocol.md#スプールの場所)）。
- Windows では、Vectorworks が動いている間は `<Plug-Ins>/cli/` そのものの名前を変えられない（中の `bin/`・`spool/`
  などは変えられる。[インストール「流れ」](plugin/install-and-update.md#流れ)）。

## 8. オーガナイザの「ファイル参照」でレイヤを選んで参照したレイヤ（分かった）

`layers` の `referenced`・`objects`・名前は、初め `ISDK::CreateReference`（レイヤを選ばない参照）の実測だけを
根拠に決めていて、実機の確認で、オーガナイザでレイヤを選んで参照したレイヤを自分のレイヤと読みました。
[#310](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/310) で調べ、参照で入るレイヤは参照の方法で 2 通りの姿になり、どちらも `IsObjectFromReferencedFile(layer)` が
真になると分かりました（[Findings「Layers and Stories」の「参照で入ってきたレイヤ」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Layers%20and%20Stories.md#参照で入ってきたレイヤ参照の方法で-2-通りの姿になる見分けは-isobjectfromreferencedfilelayer)）。
使い方は[道具「`layers`」](plugin/tools.md#layers)にあります。

## 調べないと決めたもの

利用者の判断で、SDK リファレンスで調べないと決めたものです（`CLAUDE.md` の 7 の例外）。

- **ネットワークの場所（他の計算機の共有・ネットワークのドライブ）でサーバーが届かないときに `save` が止まること。**
  OS の呼び出しがネットワークを待つことは SDK の挙動ではなく、OS が失敗を返すまで止まるものとして扱います
  （[保存先の確かめ「ネットワークの場所」](plugin/save-target.md#ネットワークの場所)）。
- **保存の途中でつながりが切れたときに、Vectorworks がダイアログを出すか。** 出ればダイアログを読む・押すコマンドで
  扱うので、出るかどうかで実装は変わりません。
