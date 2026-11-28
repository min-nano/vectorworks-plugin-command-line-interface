# SDK の調査

**SDK の挙動が不明なまま実装に着手しない**のが本リポジトリの規則です。調査するのは
[SDK リファレンス](https://github.com/min-nano/vectorworks-developer-sdk-reference)の側で、
issue を立てて `Findings/` に反映されてから、それを根拠に実装します。待機する間は SDK に依存しない
作業（通信規則・CLI・SDK に依存しない共通部）を進めます。

このページに記載するのは、調査を終えたものの索引（結果が SDK リファレンスのどこにあり、本リポジトリのどこで使用して
いるか）と、**調査しないと決定した事項**（[5](#5-同一のプラグインが-2-つ存在する場合の挙動調査しない実装を待機させない)と[末尾](#調査しないと決定した事項)）です。調査の結果そのもの（挙動・実測）は `Findings/` が真実で、ここへは
複製しません。待機中のもの・未調査の事項（未測定・未確認の挙動）は、本リポジトリの issue で扱います。
節の番号は他のページから参照しています（欠番は issue へ移した節）。

## 0. 拡張機能を 1 つも登録しないプラグインの読み込み（判明）

このプラグインはメニューも PIO も登録しません。その形式でも読み込まれて常駐することと、そのために必要な扱いが
判明しました。結果はいずれも [Findings「Plug-in Modules」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Plug-in%20Modules.md)にあります。

| 調査した事項 | 結果のある節（issue） | このプラグインでの扱い |
| --- | --- | --- |
| 拡張機能なしで読み込まれ、起動時から常駐するか | 「拡張機能を 1 つも登録しないモジュールも、有効なら起動時から常駐する」（[#252](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/252)） | [構成「拡張機能の非登録」](plugin/architecture.md#拡張機能の非登録) |
| 「開発元不明のプラグイン」のダイアログと「有効化」の記憶のされ方・ダイアログの最中に図面を操作できるか | 「開発元不明のプラグイン」のダイアログ（[#258](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/258)）・「ダイアログの最中も、プラグインは刻みから図面を扱える」（[#264](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/264)） | 同上・[インストール「インストール先のフォルダ」](plugin/install-and-update.md#インストール先のフォルダ) |
| Windows の Vectorworks が起動時にモジュールを一度解放して再度読み込むこと | 「【罠】Windows の VW は起動時にモジュールを一度降ろして読み込み直す」（[#253](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/253)） | タイマーを作成する前にモジュールを固定する（[構成「受付処理の流れ」](plugin/architecture.md#受付処理の流れ)） |
| Windows の終了時のアクセス違反（`0xC0000005`）と、新しい図面の最初の描画中に異常終了したこと | 「OS タイマーを持つ常駐プラグイン（留めたモジュール）は、終了時の落ち方を変えない」（[#257](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/257)・[#269](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/269)）・「新しい図面の最初の描画」（[#270](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/270)） | 常駐するプラグインの有無で差異が無いので、タイマーを停止する・固定の方法を変更するといった対処は追加しない（原因は判明していない）。測定方法は [Windows の実機での検証方法](plugin/windows-measurements.md) |

## 1. 他のプラグインの機能を名前で呼び出す手段（判明）

[設計「他のプラグインのツール」](design.md#他のプラグインのツール)のため。[#217](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/217) で調査し、
[Findings「Plug-in Interop」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Plug-in%20Interop.md) にまとめられました。**手段は `ISDK::CallPluginLibrary` です。**

- 採用しなかった手段: メニューコマンドの起動（`ISDK::DoMenuName`。引数も結果も渡せない）・C の ABI（`dlsym` /
  `GetProcAddress`。推奨されていない）。

## 3. 開いている図面の一覧と前面の図面の切り替え（判明）

[設計「決定事項」](design.md#決定事項)の「対象の図面」のため。[Findings「Documents」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md) にまとめられています
（実測はメニューコマンドの中と OS タイマーの周期処理の中から。Vectorworks 2026 / mac。Windows も振る舞いは同じで、相違点は
[同「Windows」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#windows振る舞いは-mac-と同じ違うのはパスの比べ方と開ける図面は-8-枚まで)。[#283](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/283)）。

| 問い | 答え |
| --- | --- |
| 開いている図面の列挙 | `GetOpenFilesList` で、文書ごとにパス・`fileRef`・前面か・未保存（`fIsInMemoryOnly`）かを取得できる。`fileRef` は連番ではない不透明な鍵 |
| 前面の切り替え | `SwitchToOpenFile(fileRef)` で切り替えられ、カレントの文書とレイヤは即座に移る |
| 前面にしないままの読み取り | 前の文書のハンドルは、別の文書が前面でも読み取れる（確認したのは名前の読み書き）。描画する場合は前面にしてからが安全 |
| 閉じている図面を開く | `OpenDocumentPath(path, false)` で開ける。既に開いているファイルは `false`。`.sta` を渡すと無題の新規文書になる |
| undo の記録 | 開く・切り替える操作では記録は開始しない。`IsCurrentlyBuildingAnUndoEvent()` は文書ごとの状態 |

- 一覧・切り替え・保存・閉じる操作の使用方法は[ツール「図面を操作するツール」](plugin/tools.md#図面を操作するツールdocumentssaveclose)に
  あります（いずれも OS タイマーの周期処理の中から呼び出せる。[#240](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/240)・[#243](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/243)）。
- 同時に開ける図面は、Windows では 8 枚までです（mac は測定していない）。

## 4. ツールが使用する SDK 呼び出し

[ツール](plugin/tools.md)のため。プラグインを 1 から記述するので、他のプラグインで動作していたことは根拠にしません。
いずれも調査を終えており、使用方法はツールのページにあります。

| ツール | 調査した事項 | 結果のある場所 | 使用方法 |
| --- | --- | --- | --- |
| `ping` | `GetCurrentLayer()` で図面が開いているかを判定する | [Timers and Notifications](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Timers%20and%20Notifications.md) の 4 の 8 | [ツール「ping」](plugin/tools.md#ping) |
| `layers` | レイヤの列挙・種類・順序・現在のレイヤ・縮尺 | [#281](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/281)。[Layers and Stories「レイヤを列挙する（デザインとシート・種類・順序・現在・縮尺）」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Layers%20and%20Stories.md) | [ツール「layers」](plugin/tools.md#layers) |
| `classes` | クラスの一覧（`ForEachClass`） | [#280](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/280)。[Attributes and Classes「クラスの一覧は `ForEachClass` で列挙する」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Attributes%20and%20Classes.md) | [ツール「classes」](plugin/tools.md#classes) |
| `layer_objects`・`object_counts` | レイヤの中のオブジェクトの列挙・計数の方法・種別・名前・クラス・外形 | [#282](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/282)。[Layer Objects](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Layer%20Objects.md) | [ツール「layer_objects・object_counts」](plugin/tools.md#layer_objectsobject_counts) |
| `documents`・`save`・`close` | 図面の一覧・指定した図面の新しいパスへの保存・保存の確認を表示せずに閉じること（mac・Windows） | [#243](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/243)・[#283](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/283)・[#305](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/305)。[Documents「指定した図面を保存する・閉じる」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#指定した図面を保存する閉じる前面に依存せず) | [ツール「図面を操作するツール」](plugin/tools.md#図面を操作するツールdocumentssaveclose) |
| `save` | 上書き保存がダイアログを表示する場面・コピーで開く図面（古い版・読み取り専用・使用中）・「すでに開かれています」の比較方法・保存後の取り消し | [#288](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/288)・[#291](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/291)。[Documents「利用者の図面を上書き保存する」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#利用者の図面を上書き保存するダイアログが出る場面と保存先の文字列288) | 同上（上書き保存は提供しない） |
| `save` | 保存先のパスの形式（UNC・`\\?\`・相対・`.`・`..`・各段の末尾の `.` と空白）・親のフォルダに書き込めない場合 | [#294](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/294)・[#299](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/299)・[#304](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/304)。[Documents「保存先のパスの形」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#保存先のパスの形unc相対-と-294) | [保存先の検証](plugin/save-target.md) |
| `save`・`close` | 大きな図面の保存と「閉じる」が呼び出しの中で完了するか・イベントを処理するか | [#290](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/290)。[Documents「大きな図面でも、呼び出しの中で終わる」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#大きな図面でも呼び出しの中で終わる290macwindows-は下の節実務の図面は測っていない) | [ツール「図面を操作するツール」](plugin/tools.md#図面を操作するツールdocumentssaveclose)・[通信規則「予約ツール」](protocol.md#予約ツール)の「長く掛かるツール」 |
| `close`・`quit` | `CloseDocument()` で閉じた図面が内部に残存し、終了時に保存の確認が表示されること | [#321](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/321)・[#323](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/323)。[Documents「閉じた図面が残り、終了で保存の確認が出る」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#閉じた図面が残り終了で保存の確認が出る321323macwindows) | [ツール「閉じた図面の後処理」](plugin/tools.md#閉じた図面の後処理) |
| `export_pdf` | シートレイヤを名前で指定し、ダイアログを出さずに PDF へ書き出すこと（`IExportPDF`）・設定・失敗の見え方・GUI のパブリッシュとの一致（mac。Windows は [#334](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/334) で確かめるまで登録しない） | [#332](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/332)。[PDF Export](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/PDF%20Export.md) | [ツール「export_pdf」](plugin/tools.md#export_pdf) |
| `quit` | 保存済みの図面の「変更あり」を読み取れるか（読み取れない） | [#240](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/240)。[Documents「未保存の変更ありは読めない」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#未保存の変更ありは読めない) | [ツール「quit」](plugin/tools.md#quit)（図面が 0 枚の場合のみ受け付ける） |
| `quit` | OS タイマーの周期処理から `CloseAllFilesAndQuitVectorworks(true, false)` を呼び出すこと・保存の確認の最中の周期処理・復帰後の 2 度目 | [#230](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/230)・[#235](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/235)・[#313](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/313)。[Timers and Notifications](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Timers%20and%20Notifications.md) の 4 の 9 | [構成「Vectorworks が処理中」](plugin/architecture.md#vectorworks-が処理中) |
| `quit` | 起動の途中に終了を依頼した場合の挙動・起動完了の目印 | [#312](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/312)・[#317](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/317)。同 4 の 9「起動の途中の刻みから」「起動を終えた目印」 | [ツール「quit」](plugin/tools.md#quit)・[構成「エントリポイント」](plugin/architecture.md#エントリポイント) |

## 5. 同一のプラグインが 2 つ存在する場合の挙動（調査しない。実装を待機させない）

同一のプラグイン（同一のユニバーサル名・拡張機能の UUID・リソースの識別子を登録するモジュール）が
`Plug-Ins` の 2 か所に存在する場合、Vectorworks が何をもって衝突とみなし、どう振る舞うかは
SDK リファレンスの `Findings/` に記載がありません。`vw2026 install` はこれを判定せず、二重に
インストールしないことを利用者の責任としたので（[レジストリ「提供側への要件」](registry.md#提供側への要件)）、
実装はこの調査を待機しません。判定を追加することになった場合は、先に SDK リファレンスで issue を立てます。

## 6. プラグインのフォルダ内のモジュール以外のファイルと、モジュールの読み込み元（判明）

`<Plug-Ins>/cli/` にはモジュールの他に `bin/`（CLI の実行ファイル）・`hooks/`・`build.json`・
`vw2026-install.json`・`spool/` が格納されます（[インストール「CLI とスプールの配置場所」](plugin/install-and-update.md#cli-とスプールの配置場所)）。
[#277](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/277) で調査し、[Findings「Plug-in Modules」の「プラグインのフォルダの『モジュール以外のもの』は無視され、モジュールは置いた場所から読まれる」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Plug-in%20Modules.md#プラグインのフォルダのモジュール以外のものは無視されモジュールは置いた場所から読まれるwindowsmacos)に
まとめられました（Windows も mac も同じ）。本リポジトリが依存しているのは次の 3 点です。

- Vectorworks は `Plug-Ins` の下のモジュール以外のファイルとサブフォルダを無視する（スプールに前回の残りが
  残存したまま起動しても同じ）。
- Vectorworks はモジュールを配置した場所のファイルからそのまま読み込むので、プラグインがモジュールの場所から導出する
  スプールは `<Plug-Ins>/cli/spool/` になる（[通信規則「スプールの場所」](protocol.md#スプールの場所)）。
- Windows では、Vectorworks が動作している間は `<Plug-Ins>/cli/` そのものの名前を変更できない（内部の `bin/`・`spool/`
  などは変更できる。[インストール「処理の流れ」](plugin/install-and-update.md#処理の流れ)）。

## 8. オーガナイザの「ファイル参照」でレイヤを選択して参照したレイヤ（判明）

`layers` の `referenced`・`objects`・名前は、当初 `ISDK::CreateReference`（レイヤを選択しない参照）の実測のみを
根拠に決定しており、実機の確認で、オーガナイザでレイヤを選択して参照したレイヤを自身のレイヤとして読み取りました。
[#310](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/310) で調査し、参照で取り込まれるレイヤは参照の方法により 2 通りの形態になり、いずれも `IsObjectFromReferencedFile(layer)` が
真になることが判明しました（[Findings「Layers and Stories」の「参照で入ってきたレイヤ」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Layers%20and%20Stories.md#参照で入ってきたレイヤ参照の方法で-2-通りの姿になる見分けは-isobjectfromreferencedfilelayer)）。
使用方法は[ツール「`layers`」](plugin/tools.md#layers)にあります。

## 調査しないと決定した事項

利用者の判断で、SDK リファレンスで調査しないと決定した事項です（`CLAUDE.md` の 7 の例外）。

- **ネットワークの場所（他の計算機の共有・ネットワークのドライブ）でサーバーに到達できない場合に `save` が停止すること。**
  OS の呼び出しがネットワークを待機することは SDK の挙動ではなく、OS が失敗を返すまで停止するものとして扱います
  （[保存先の検証「ネットワークの場所」](plugin/save-target.md#ネットワークの場所)）。
- **保存の途中で接続が切断された場合に、Vectorworks がダイアログを表示するか。** 表示されればダイアログを読み取る・押すコマンド（[#133](https://github.com/min-nano/vectorworks-plugin-command-line-interface/issues/133)）で
  扱うので、表示されるかどうかで実装は変わりません。
