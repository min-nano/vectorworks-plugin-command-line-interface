# SDK の調査

**SDK の挙動が分からないまま実装に入らない**のがこのリポジトリの決めごとです。調べるのは
[SDK リファレンス](https://github.com/min-nano/vectorworks-developer-sdk-reference)の側で、
issue を立てて `Findings/` に反映されてから、それを根拠に実装します。待つ間は SDK に依らない
作業（通信規則・CLI・SDK に依らない共通部）を進めます。

## 0. 拡張機能を 1 つも登録しないプラグインの読み込み（分かった）

このプラグインはメニューも PIO も登録しません（[構成](plugin/architecture.md#拡張機能を登録しない)）。
その形でも Vectorworks が読み込み、`plugin_module_main` を呼ぶか（＝OS タイマーを開始できるか）は
SDK リファレンスに記載がありませんでした。

- **macOS は読み込まれた**（段 2 の実機確認。VW 2026。起動の直後から OS タイマーが刻み、`call ping`
  が応えた。下の「開発元不明のプラグイン」のダイアログで、先に「有効化」が要ることがある）。
  結果は [Findings「Plug-in Modules」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Plug-in%20Modules.md)の「拡張機能を 1 つも登録しないモジュールも、有効なら
  起動時から常駐する」に反映された（[SDK リファレンス #252](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/252)）。
- **Windows も、留めた版（下記）は読み込まれ常駐した**（段 2 の実機確認。VW 2026 x64・Windows 11
  ARM64 の x64 エミュレーション）。起動の直後から `cli.vlb` がプロセスに居て、`call ping` が応えた。
  拡張機能を登録しない形のままでよい。
- **Windows の Vectorworks が閉じたときにアクセス違反（`0xC0000005`）で終わったのは、このプラグインの
  せいではないと読める（推定）。** 段 2 の実機確認で、すでに外された `libcef.dll` の中で落ちた回があった。
  [SDK リファレンス #257](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/257) で調べ、留めた常駐プラグインの有無で終わり方に差は無く、
  何が効いていたかは分かっていない（[Findings「Plug-in Modules」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Plug-in%20Modules.md)の
  「OS タイマーを持つ常駐プラグイン（留めたモジュール）は、終了時の落ち方を変えない」）。タイマーを止める・
  留め方を変えるといった作法は要らない。`cli` の入っていない構成でも同じ落ち方が出ており
  （[#269](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/269)）、原因は分かっていない。新しい図面の最初の描画中に `MaxonDocumentBuilder.vlb` の中で
  落ちたことも、常駐するプラグインの有無で差が無かった（[#270](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/270)。同じページの「新しい図面の最初の描画」）。
  段 2 の確かめ方は [Windows の実機での確かめ方](plugin/windows-measurements.md)。
- **Vectorworks は、開発元の分からないプラグインを読み込まないことがある（Windows も macOS も）。**
  起動の途中で「開発元不明のプラグイン」のダイアログが出て `cli` を「無効」と示し、その回は読み込まなかった。
  ダイアログで「有効化」を選ぶと、次の起動から読み込まれた。**ファイルを入れ替えても「有効化」は
  残った**（`296a797` へ入れ替えた後の起動でダイアログは出たが、`cli` は「有効」のまま読み込まれ、
  ダイアログが開いている間も `call ping` が応えた）。ダイアログは毎回は出なかった。macOS でも同じダイアログで
  止められ、「有効化」して起動し直した後から読み込まれたことがある。[SDK リファレンス #258](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/258) で
  **分かった**（[Findings「Plug-in Modules」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Plug-in%20Modules.md)
  の「開発元不明のプラグイン」のダイアログ。Windows も macOS も同じ）。「有効化」はモジュールの
  **フルパス**で覚え、中身は見ない。同じパスのままフォルダごと付け替えても残り、フォルダ名を
  変えると「無効」に戻る。`vw2026 install` はフォルダ名をレジストリの名前に固定し（`cli` は
  `<Plug-Ins>/cli`）、更新では同じパスへ付け替えるので、この結果のとおりに残る
  （[インストール「入れる先のフォルダ」](plugin/install-and-update.md#入れる先のフォルダ)）。
- 全部有効なときに約 24 時間おきに出るダイアログの最中に図面を扱えるかは、
  [SDK リファレンス #264](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/264) で
  **分かった**（[Findings「Plug-in Modules」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Plug-in%20Modules.md)の「ダイアログの最中も、プラグインは刻みから図面を扱える」「mac も、共通モードの刻みからなら扱える」）。
  どう扱うかは[構成「拡張機能を登録しない」](plugin/architecture.md#拡張機能を登録しない)。
- 留めていなかった版で常駐しなかったのは、このダイアログで止められていた（有効化の前だった）ためと
  読める（[SDK リファレンス #252](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/252)。[Findings「Plug-in Modules」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Plug-in%20Modules.md)の
  「拡張機能を 1 つも登録しないモジュールも、有効なら起動時から常駐する」）。留めない形が下の読み込み直しの
  対象になるかは確かめられていないが、このプラグインは留めるので困らない。
- 同じ確認の途中で、利用者の `Plug-Ins` にプラグインを置くと Vectorworks が起動画面のあと記録を
  残さずに終了することがあった（このプラグインに限らない）。
  [SDK リファレンス #253](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/253) で
  **分かった**（[Findings「Plug-in Modules」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Plug-in%20Modules.md)
  の「【罠】Windows の VW は起動時にモジュールを一度降ろして読み込み直す」）。Windows の
  Vectorworks は起動時にモジュールを `kPluginModuleInit` のあと `kPluginModuleDeinit` を送らずに
  一度降ろし、別の番地へ読み込み直す。留めずに作った `SetTimer` のタイマーはもう無い番地を呼び続け、
  そこへ後から入った別のモジュールのコードを踏んで落ちる。落ちた回は Windows の記録に残らず、
  Vectorworks 同梱の Sentry がダンプを書く。**タイマーを作る前に `GET_MODULE_HANDLE_EX_FLAG_PIN` で
  自分を留める**ことにした（[構成「受け付けの流れ」](plugin/architecture.md#受け付けの流れ)）。macOS は
  読み込み直さないので留めない。留めた版は段 2 の実機確認で起動の途中に落ちなかった。

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

## 3. 開いている図面の一覧と、前面の図面の切り替え（分かった）

[設計「決めたこと」](design.md#決めたこと)の「対象の図面」のため。[Findings「Documents」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md) にまとまっています
（実測はメニューコマンドの中と OS タイマーの刻みの中から。VW 2026 / macOS。Windows は [#283](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/283)）。

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
- **OS タイマーの刻みの中から呼べる。** 未保存の判定の調査（[#240](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/240)）と `save`・`close` の調査
  （[#243](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/243)）で、一覧（`GetOpenFilesList`）・切り替え（`SwitchToOpenFile`）・開く
  （`OpenDocumentPath`）・保存・「閉じる」を刻みの中から呼んで確かめた（[Findings「未保存の変更ありは読めない」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#未保存の変更ありは読めない)・
  [「指定した図面を保存する・閉じる」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#指定した図面を保存する閉じる前面に依存せず)）。

- **Windows も振る舞いは mac と同じ**（[#283](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/283)。[Findings「Documents」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#windows振る舞いは-mac-と同じ違うのはパスの比べ方と開ける図面は-8-枚まで)の
  「Windows——振る舞いは mac と同じ。違うのはパスの比べ方と、開ける図面は 8 枚まで」）。違うのは次のとおり。
  使い方は[道具「図面を扱う道具」](plugin/tools.md#図面を扱う道具documentssaveclose)。
  - 図面のパスは、保存したときに渡した文字列がそのまま残る（区切りだけ `\` に揃う。ドライブ文字・名前の大小・
    8.3 の短い名前は直らない）。
  - 「すでに開かれています」の警告は、開いている図面のパスと文字列が完全に一致するときだけ出る。既にあるファイルへは
    どの形で渡しても保存できず、自分のパスへの上書き保存も文字列が完全に一致するときだけ通る。
  - 「閉じる」の保存の確認の最中に、呼んだ `SetTimer` の刻み自身が入り直す（mac は入り直さない）。
  - 開いていられる図面は 8 枚まで（mac は測っていない）。260 文字を超えるパスへは保存できない。

## 4. 最初の道具が使う SDK 呼び出し

[最初に提供する道具](plugin/tools.md)のため。プラグインを 1 から書くので、ほかのプラグインで
動いていたことは根拠にしません。SDK リファレンスの `Findings/` に無いものを確かめます。

| 道具 | 呼び出し | リファレンス |
| --- | --- | --- |
| `layers` | レイヤの列挙（`FirstMemberObj(GetDrawingHeader())`＋`NextObject`、または `ForEachLayer`） | **分かった**（[#281](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/281)。[Layers and Stories「レイヤを列挙する（デザインとシート・種類・順序・現在・縮尺）」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Layers%20and%20Stories.md)。Windows）。シートレイヤも同じ列に作った順で並ぶ（オーガナイザはデザインレイヤだけ逆順に見せる）。列の末尾に型 `0` が 1 つ居るので `kLayerNode`（31）で絞る。種類は `ovLayerType`（デザイン `1`・シート `2`・参照で入ったデザインレイヤ `3`。名前は `NNA#<n>_<元の名前>`）。現在のレイヤは `GetCurrentLayer()` とハンドルの `==` で判定できる。縮尺は `GetLayerScaleN` で分母（1/50 なら `50`）、シートレイヤは常に `1`。刻みの中・図面 0 枚でも落ちない（列が空になる）。使い方は[道具「layers」](plugin/tools.md#layers) |
| `classes` | クラスの一覧（`ForEachClass`） | **分かった**（[#280](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/280)。[Attributes and Classes「クラスの一覧は `ForEachClass` で列挙する」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Attributes%20and%20Classes.md)。Windows）。前面の図面のクラスだけを ID の順（作った順）に渡す（オーガナイザは名前の昇順）。`doGuestClasses` が真なら参照先のクラスが `NNA#<n>_<名前>` で後ろに足される（オーガナイザには出ない）。階層名（`-` 区切り）は 1 つのクラスで、親は作られない。刻みの中・図面 0 枚でも落ちない（0 件）。使い方は[道具「classes」](plugin/tools.md#classes) |
| `layer_objects` / `object_counts` | レイヤの中のオブジェクトの列挙・種別・名前・クラス・外形 | **分かった**（[#282](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/282)。[Layer Objects](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Layer%20Objects.md)。mac・Windows）。`FirstMemberObj` → `NextObject` は作った順で、先頭が最背面。列には終端（型 `0`）と undo の置き石（型 `90`。並べ替え・消去・取り消しで増え、保存しても残ることがある）が混ざるので除く（`ForEachObjectN` は渡さない）。情報パレットの「N 図形」はレイヤの直下の数（グループ・シンボル・PIO・壁は 1 つ）で、オーガナイザに数の列は無い。種別の番号から名前を引く口は SDK に無い（自前の表。PIO は `GetParametricName`）。名前の無い図形の名前は空文字列。クラスは `GetObjectClass` → `ClassIDToName`。外形は縮尺を掛けない mm の外接矩形（文字は紙の上の大きさ×縮尺）。シートレイヤにはビューポート（型 `122`）が居る。刻みの中から辿ってよい。使い方は[道具「layer_objects・object_counts」](plugin/tools.md#layer_objectsobject_counts) |
| `ping` | `GetCurrentLayer()` で図面が開いているかを判定する | **分かった**（[Timers and Notifications](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Timers%20and%20Notifications.md) の 4 の 8。使い方は[道具「ping」](plugin/tools.md#ping)） |
| `save` / `close` / `documents` | 図面の一覧（`GetOpenFilesList`）・指定した図面の保存（`SwitchToOpenFile` → `SaveActiveDocumentPath`。無題の図面を新しいパスへ）・メニュー「閉じる」（`DoMenuName("Close", 0)`） | 一覧・同じパスへの上書き保存・「閉じる」で保存の確認が出ることは [Findings「Documents」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md) にある。**分かった**（[#243](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/243)）。新しいパスへは通る・既存のファイルへは通らず（開いているファイルなら警告のダイアログが出る）・前面は戻せる・保存した直後なら確認なしに閉じる。Windows も同じ（[#283](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/283)。違うのはパスの比べ方。上記 3）。使い方は[道具](plugin/tools.md#図面を扱う道具documentssaveclose)。上書き保存がダイアログを出す場面（ファイルが無い・開いた後で読み取り専用になった）・古い版・読み取り専用・使用中のファイルはコピーで開くこと（`fIsInMemoryOnly` が真）・`to` の形・「すでに開かれています」の比べ方（mac も文字列の完全一致）・保存のあとの取り消しも**分かった**（[#288](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/288)。Windows の残りは [#291](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/291) で**調べている**）。保存先のパスの形（UNC・`\\?\`・ドライブ相対・ルート相対・`.`・`..`）も**分かった**（[#294](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/294)。[Findings「保存先のパスの形」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#保存先のパスの形unc相対-と-294)。使い方は[道具](plugin/tools.md#図面を扱う道具documentssaveclose)）。大きな図面の「閉じる」と保存も**分かった**: 呼び出しの中で終わり、ダイアログが出ない限りイベントを回さない。時間は図形の数に比例し、描画したあとに閉じると数倍かかる（[#290](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/290)。仮想マシンの合成の図面で測り、実務の図面は測っていない） |
| `quit`（未保存の判定） | 保存済みの図面の**変更あり**を読む | **分かった: 読めない**（[#240](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/240)。[Findings「未保存の変更ありは読めない」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#未保存の変更ありは読めない)）。`outSaved`・`fIsInMemoryOnly` は「ディスクにあるか」だけを表す。そのため `quit` は図面が 0 枚のときだけ受け付ける設計にした（[道具「quit」](plugin/tools.md#quit)） |
| `quit` | OS タイマーの刻みから `CloseAllFilesAndQuitVectorworks(true, false)` を呼ぶ | **分かった**（[#230](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/230)・[#235](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/235)。[Findings「Timers and Notifications」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Timers%20and%20Notifications.md) の 4 の 9・[Documents「`CloseAllFilesAndQuitVectorworks`（終了）」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md)）。刻みから呼べ、図面を閉じ終えてから戻り、終了はその後のイベントループで起きる（mac・Windows）。保存の確認を取り消すと呼び出しは戻り、図面は残る。確認の最中にもう一度呼ぶと確認が 2 つ重なって出る（落ちない）。Windows では呼んだタイマー自身が確認の最中に入り直す（Findings は「終了の呼び出しの最中」を旗で持って見送る作りを勧める。このプラグインでどうするかは段 6 の前に決める）。利用者が開いたモーダルの最中も落ちない。終了の依頼の最中の判定は残し、入れ子の判定にまとめるかを段 6 の前に決める（[設計のレビュー A4](design-review.md#a4-終了の依頼の最中に受け付けが入れ子になりうる)） |

## 5. 同じプラグインが 2 つあるときの挙動（調べていない。実装を待たせない）

同じプラグイン（同じユニバーサル名・拡張機能の UUID・リソースの識別子を登録するモジュール）が
`Plug-Ins` の 2 か所にあるとき、Vectorworks が何をもって衝突とみなし、どう振る舞うかは
SDK リファレンスの `Findings/` に記載がありません。`vw2026 install` はこれを判定せず、二重に
入れないのを利用者の責任としたので（[レジストリ「提供側に求めること」](registry.md#提供側に求めること)）、
実装はこの調査を待ちません。判定を足すことになったら、先に SDK リファレンスで issue を立てます。

## 6. プラグインのフォルダの中のモジュール以外のものと、モジュールの読み込み元（分かった）

段 3（3a・3b）から、`<Plug-Ins>/cli/` にはモジュールのほかに `bin/`（CLI の実行ファイル）・`hooks/`（`.sh` /
`.ps1`）・`build.json`・`vw2026-install.json`・`spool/`（`bridge.lock`・`*.req.json`・`*.res.json`・
`*.wait`・`*.tmp`）が入ります（[インストール「CLI とスプールの置き場所」](plugin/install-and-update.md#cli-とスプールの置き場所)）。
[SDK リファレンス #277](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/277) で
**分かった**（[Findings「Plug-in Modules」の「プラグインのフォルダの『モジュール以外のもの』は無視され、モジュールは置いた場所から読まれる」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Plug-in%20Modules.md#プラグインのフォルダのモジュール以外のものは無視されモジュールは置いた場所から読まれるwindowsmacos)。
Windows も macOS も同じ）。

- **Vectorworks は `Plug-Ins` の下のモジュール以外のファイルとサブフォルダを無視する。** 起動時に
  警告を出さず（「開発元不明のプラグイン」のダイアログにもモジュールしか載らない）、起動をまたいで
  書き換え・移動・削除をせず、起動後に開いたままにもしない。スプールに `bridge.lock` と前の回の残りが
  入ったまま次の起動を迎えても同じだった。
- **Vectorworks はモジュールを置いた場所のファイルからそのまま読み込む。** mac の `dladdr`、Windows の
  `GetModuleHandleExW(FROM_ADDRESS)` → `GetModuleFileNameW` は `<Plug-Ins>/cli/` の中のモジュールを返す。
  プラグインがモジュールの場所から求めるスプール（[通信規則「スプールの場所」](protocol.md#スプールの場所)）は
  `<Plug-Ins>/cli/spool/` になる。Windows で起動時に一度降ろして読み込み直した（上記 0）あとも同じパスだった
  （降ろす前の最初の読み込みで求めた値は見ていない。同じファイルからの読み直しなので同じと推定されている）。
- **Windows では、Vectorworks が動いている間は `<Plug-Ins>/cli/` そのものの名前を変えられない**
  （`cli.vlb` が写像されているため。中の `bin/`・`spool/` などのフォルダは変えられる）。`vw2026 install` が
  フォルダごと付け替えるのは Vectorworks が止まっているときだけ、という決めごと（[インストール「流れ」](plugin/install-and-update.md#流れ)）と
  合っている。

段 3a の実機確認でも、スプールが既にある状態での起動を確かめます（[実装の順序](plugin/plan.md)の段 3a）。
