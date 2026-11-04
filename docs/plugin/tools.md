# 最初に提供する道具

図面を**読むだけ**の道具と、図面を保存して閉じる道具（`documents`・`save`・`close`）、Vectorworks を終わらせる 1 つ（`quit`）から始めます。最初の
リリース（[実装の順序](plan.md)の段 2）に載せるのは `ping` だけで、共通部の変更は段 4、読む道具と図面を扱う道具は段 5、`quit` は段 6 で
足します。更新は道具ではなく CLI の `vw2026 install` です（[インストール](install-and-update.md#インストールと更新vw2026-install)）。**SDK 呼び出しは、SDK リファレンスで
挙動が確かめられているものだけを使います**（無いものはリファレンスで調べてから足す。[SDK の調査](../sdk-research.md)）。

引数は、省略できるものに既定値を設けます（表の「既定」）。知らない引数と、知っている引数の
未知の値（型の違い・決まった綴り以外の値）は `invalid_args` で返します（[通信規則「互換性」](../protocol.md#互換性)）。

**道具が返す失敗の `invalid_args` と `internal` は、次のように分けます**（[通信規則「応答」](../protocol.md#応答idresjson)）。

- **`invalid_args`** は、引数そのものが誤っているとき: 知らない引数・型や値の誤り・必須の欠け、引数が名指すもの
  （`document` の図面・`layer` のレイヤ）が実在しない、道具の規則に反する組み合わせ（保存済みの図面への `save` の `to`）。
- **`internal`** は、引数は正しく、名指すものも実在するが、それ以外の状態のために実行できないとき: 図面が無い
  （`no_document`）・図面が開いている（`quit` の `documents_open`）・保存先が埋まっている（`save` の `to_is_open`・
  `file_exists`。`to` は新しく作るファイルを名指すので、既にあることは状態に当たる）・図面が閉じなかった（`close` の
  `not_closed`。保存の確認が取り消されたなど）・SDK の失敗（`save` の `save_failed`）・道具の中の例外。

呼ぶ側が理由で分岐するときは、`code` ではなく `detail` の `reason` を見ます（`reason` を返す失敗だけ）。

| 道具 | 種類 | 引数 | 結果 |
| --- | --- | --- | --- |
| `tools` | （予約） | なし | [ブリッジ「道具の表」](bridge.md#道具の表) |
| `ping` | read | なし | 下記 |
| `layers` | read | `include_sheets`（既定 true） | `{"layers":[{"name","kind":"design"\|"sheet","referenced","scale","objects","current"}],"count"}`（[読む道具](#読む道具layersclasseslayer_objectsobject_counts)） |
| `classes` | read | なし | `{"classes":[名前…],"count"}`（同上） |
| `layer_objects` | read | `layer`（必須）・`limit`（既定 50）・`offset`（既定 0）・`type`（種別の番号。省略＝すべての種別） | `{"layer","unit":"mm","objects":[{"index","type","type_name","pio_name","name","class","bounds":{"left","right","top","bottom"}}],"returned","total"}`（同上） |
| `object_counts` | read | `layer`（省略＝図面全体） | `{"layer"?,"types":[{"type","type_name","count"}],"total"}`（種別番号の昇順。同上） |
| `documents` | read | なし | [図面を扱う道具](#図面を扱う道具documentssaveclose) |
| `save` | app | `document`（必須）・`to`（省略可） | 同上 |
| `close` | app | `document`（必須） | 同上 |
| `quit` | app | なし | `{"quitting":true}`。図面が開いていれば `internal` で失敗し、`detail` に一覧（下記） |

## `ping`

```json
{"version":"abc1234","branch":"main","protocol":6,"pid":4242,"session":false,"document_open":true,"current_layer":"1F"}
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
{"version":"abc1234","branch":"main","protocol":6,"pid":4242,"session":false,"document_open":false,"current_layer":null}
```

例の `protocol` は書いた時点の版です（[通信規則「版」](../protocol.md#版)）。

## 読む道具（`layers`・`classes`・`layer_objects`・`object_counts`）

図面を読むだけの道具です（[実装の順序](plan.md)の段 5）。使う SDK 呼び出しと挙動は SDK リファレンスで
確かめてあります（[SDK の調査 4](../sdk-research.md#4-最初の道具が使う-sdk-呼び出し)）。どれも前面の図面
（アクティブな図面）を読みます。

**共通の決めごと:**

- **図面が開かれていなければ `internal` で失敗し、`detail` に `{"reason":"no_document"}` を付けます。**
  SDK は図面が無くても落ちずに空の列を返します（[Findings「Layers and Stories」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Layers%20and%20Stories.md)の「OS タイマーの刻みの中・図面 0 枚」・
  [Findings「Attributes and Classes」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Attributes%20and%20Classes.md)の「タイマーの刻みから呼べる。図面 0 枚でも落ちない」）が、
  空の結果（`object_counts` の `total:0` など）を「中身の無い図面」と見分けられなくなるので、失敗にします。
  `documents` は失敗にせず、空の一覧を返します。`ping` は `current_layer` を `null` にします（[`ping`](#ping)）。
- **`layer` はレイヤの名前で、`layers` の `name` と完全に一致するものを探します**（参照で入ってきたレイヤは
  `NNA#1_<元の名前>` の形のまま渡す）。一致しなければ `invalid_args` で断ります。図面が無いときは、`layer` を
  確かめる前に `no_document` で失敗します。
- **並び順は SDK の列の順で、背面→前面です**（`layers` も `layer_objects` も同じ向き）。画面の並びに
  合わせて並べ替えることはしません（呼ぶ側が並べ替える）。

### `layers`

`GetDrawingHeader()` → `FirstMemberObj` → `NextObject` の列を、型が `kLayerNode`（31）のものだけに絞って返します
（列の末尾に、レイヤではない型 0 のものが 1 つ居る。[Findings「Layers and Stories」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Layers%20and%20Stories.md)の
「レイヤを列挙する」）。

| フィールド | 値 |
| --- | --- |
| `name` | レイヤの名前（`GetObjectName`）。参照で入ってきたレイヤは `NNA#1_<元の名前>` |
| `kind` | `design`（`ovLayerType` が 1 か 3）・`sheet`（2）。それ以外の値（Findings で確かめていない種類）は `null` |
| `referenced` | 参照で入ってきたデザインレイヤか（`ovLayerType` が 3） |
| `scale` | 縮尺の分母（`GetLayerScaleN`。1/50 なら `50`、1:1 なら `1`）。**シートレイヤは SDK の返すとおり常に `1`** |
| `objects` | レイヤの直下の図形の数（下記「数え方」） |
| `current` | 現在のレイヤか（`GetCurrentLayer()` とハンドルが一致するか） |

- **並びは作った順で、デザインレイヤとシートレイヤが入り混じります。** オーガナイザはデザインレイヤだけを
  この逆の順（`#` の 1 が最前面）に見せ、シートレイヤはこの順のまま見せます
  （[Findings「Layers and Stories」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Layers%20and%20Stories.md)の「順序」）。
- `include_sheets` が偽なら、`kind` が `sheet` のものを除きます（`kind` が `null` のものは除かない）。
- **`kind` が `null` のレイヤも除かずに返し**、`objects` も数えます。`object_counts` の図面全体の和と食い違わない
  ようにするためです。
- **参照で入ってきたレイヤも返します**（`referenced` が真）。その直下には図形が見えないので、`objects` は 0 に
  なります。

### `classes`

`ForEachClass(false, …)` で、前面の図面のクラスの名前を返します（[Findings「Attributes and Classes」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Attributes%20and%20Classes.md)の
「クラスの一覧は `ForEachClass` で列挙する」）。

- **参照している図面のクラス（`NNA#<n>_…`）は含めません**（`doGuestClasses` は偽）。オーガナイザと同じ顔ぶれです。
- **並びはクラスの ID の順（作った順）です。** オーガナイザは名前の順に見せますが、その並べ方を真似ずに、
  呼ぶ側が並べ替えます。
- 階層の名前（`-` 区切り）は 1 つのクラスの名前のまま返します（親のクラスは実在しない）。

### `layer_objects`・`object_counts`

**数え方:** レイヤの**直下**の図形を数えます。グループ・シンボル・PIO・壁などは中身を数えずに 1 つです。
`FirstMemberObj` → `NextObject` で辿り、終端（型 0）と undo の置き石（型 90。並べ替え・消去・取り消しで増え、
保存しても消えないものがある）を除きます。これが情報パレットの「すべてを選択」の「N 図形」と一致します
（オーガナイザに数の列は無い。[Findings「Layer Objects」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Layer%20Objects.md)の「まとめ」・2・3）。
グループの中へ降りる指定は持ちません。

- **`object_counts` の図面全体は、`layers`（`include_sheets` が真）が返すすべてのレイヤの直下の数の和です。**
  シートレイヤ（ビューポートなど）を含み、シンボルの定義（どのレイヤにも属さない）は含みません。
- **`layer_objects` に `type` を渡すと、その種別の図形だけに絞ってから数えます。** `offset` は絞ったあとの
  並びで飛ばす数、`returned` は返した数、`total` は絞ったあとの数です（`offset` と `limit` で頁を送れるように）。
- **`index`** は、レイヤの直下の並び（背面→前面。型 0 と 90 を除く）の中の 0 からの位置で、`type` で絞っても
  変わりません（絞った結果どうしでも前後関係を比べられるように）。消去・取り消し・並べ替えで変わるので、
  図形の識別子には使えません。
- **`limit` は 1〜1000、`offset` は 0 以上、`type` は 0 以上の整数です。** 数は整数の綴り（小数部と指数部が
  無い）だけを受け、`2.0` や範囲の外は `invalid_args` で断ります。`offset` が `total` 以上なら `objects` は空で
  成功します。表に無い番号の `type` も断らず、合う図形が無ければ `total` は 0 です。`limit` に上限を設けるのは、
  1 回の受け付けと応答を大きくしすぎないためです。
- **`type`** は `GetObjectTypeN` の番号です（引数の `type` も番号）。**`type_name`** は、番号から名前を引く口が
  SDK に無いので（[Findings「Layer Objects」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Layer%20Objects.md)の 5）、
  プラグインが自分の表（下記）で引きます。表に無い番号は `null` です。
- **`type_name` の表。** 綴りはこの表が真実です。レイヤの直下か図形の中に出ることを Findings で確かめた種別だけを
  載せます（[Findings「Layer Objects」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Layer%20Objects.md)の 5。番号と定数は `Objs.TDType.h`）。
  見本で出たシンボル定義（16）は、どのレイヤにも属さず `layer_objects` に出ないので載せません。綴りは定数の
  名前から先頭の `k` と末尾の `Node` を除いて作りましたが、決めるのは規則ではなくこの表です。**載せた綴りは
  変えません。** 表に行を足す（`null` だった番号に名前を付ける）のは互換の変更として行えるので、呼ぶ側は `null` を
  扱います。

  | `type` | 定数 | `type_name` |
  | --- | --- | --- |
  | 2 | `kLineNode` | `line` |
  | 3 | `kBoxNode` | `box` |
  | 4 | `kOvalNode` | `oval` |
  | 5 | `kPolygonNode` | `polygon` |
  | 6 | `kArcNode` | `arc` |
  | 10 | `kTextNode` | `text` |
  | 11 | `kGroupNode` | `group` |
  | 15 | `kSymbolNode` | `symbol` |
  | 17 | `kLocusNode` | `locus` |
  | 21 | `kPolylineNode` | `polyline` |
  | 24 | `kExtrudeNode` | `extrude` |
  | 68 | `kWallNode` | `wall` |
  | 84 | `kCSGTreeNode` | `csg_tree` |
  | 86 | `kParametricNode` | `parametric` |
  | 122 | `kViewportNode` | `viewport` |

- **`pio_name`** は PIO（`type` が 86）の名前（`VWParametricObj::GetParametricName()`。`Door` など）で、PIO で
  なければ `null` です。
- **`name`** は `GetObjectName` のままで、名前の無い図形は空文字列（`""`）です（Vectorworks でも空の名前と
  区別が無い）。**`class`** はクラスの名前（`GetObjectClass` → `ClassIDToName`）です。
- **`bounds`** は `GetObjectBounds` の外形で、**単位は常に mm**です。結果の **`unit`** は常に `"mm"` で、
  単位の取り違えを防ぐために置きます。値は図面の単位にもレイヤの縮尺にもよらない世界座標です。外形が取れない
  図形は `null` です。
  - **`left ≤ right`・`bottom ≤ top` です。** `left`・`right` は X の小さい側と大きい側、`bottom`・`top` は Y の
    小さい側と大きい側で、プラグインが SDK の外形から最小と最大をとって入れます（画面の座標のように Y が下向きに
    増えるとは読まない）。
  - 回した図形は外接矩形、3D の図形は平面の外接矩形です。
  - 文字の大きさは、作ったときのレイヤの縮尺で決まります（1:50 のレイヤで作った文字は 50 倍。
    [Findings「Layer Objects」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Layer%20Objects.md)の 6）。
  - シートレイヤの図形（ビューポートなど）は紙の上の mm です。

## `quit`

CLI はプログラムやスクリプトから使うので、**保存の確認を出さない**ことを `quit` の前提にします。
保存済みの図面の未保存の変更は SDK から読めず（[Findings「未保存の変更ありは読めない」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#未保存の変更ありは読めない)。
`outSaved` も `fIsInMemoryOnly` も「ディスクに保存されたことがあるか」を表すだけ）、無題の図面は変更が無くても
終了で保存の確認が出ます。そこで **`quit` は、図面を 1 枚も開いていないときだけ受け付けます**。
開いている図面を閉じておくのは呼ぶ側の役割です。

- **図面が 1 枚でも開いていれば、終了を頼まずに `internal` で失敗します。** 受け付けの中で
  `GetOpenFilesList` の件数を読みます（図面が 0 枚のときも落ちない。[道具「ping」](#ping)）。
  `error` は「図面が開いているので終了しませんでした。先に図面を閉じてください」を含む固定の文言で、
  `code` は通信規則の共通の種別のまま（道具ごとの種別は足さない。[通信規則「応答」](../protocol.md#応答idresjson)）です。
- **失敗の応答の `detail`** に、開いている図面の一覧を載せます（[通信規則「応答」](../protocol.md#応答idresjson)の
  `detail`。呼ぶ側が閉じるべき図面を知るため）。

  ```json
  {"ok":false,"code":"internal","error":"図面が開いているので終了しませんでした。先に図面を閉じてください","detail":{"reason":"documents_open","documents":[{"name":"plan.vwx","path":"/Users/me/plan.vwx","untitled":false},{"name":"名称未設定 1","path":"/Applications/Vectorworks 2026/名称未設定 1","untitled":true}]}}
  ```

  | フィールド | 値 |
  | --- | --- |
  | `reason` | `documents_open`（`quit` の失敗の理由を表す固定の綴り。呼ぶ側はこれで分岐する） |
  | `documents[].name` | 図面の名前 |
  | `documents[].path` | 図面のパス（`documents` と同じ。無題の図面は「アプリケーションのフォルダ＋名称未設定 N」でファイルとしては存在しない） |
  | `documents[].untitled` | 一度も保存していない図面か |
- **図面が 0 枚なら、保存の確認は出ません。** 応答を書いてから、受け付けの外で
  `gSDK->CloseAllFilesAndQuitVectorworks(true, false)` を頼みます（[構成「受け付けの流れ」](architecture.md#受け付けの流れ)）。
  第 1 引数は常に真です（**利用者の図面を保存せずに閉じる手段は持たない**）。確かめてから頼むまでの間に利用者が
  図面を開くと、保存の確認が出うる競合が残りますが、`quit` は占有の中でだけ呼べ、呼ぶ側が図面を閉じた直後に
  呼ぶ使い方なので、通常は起きません。
- **図面を保存して閉じるのは呼ぶ側です。** 保存するか捨てるかも呼ぶ側が決めます（`quit` に保存する・捨てる指定は持たず、
  CLI にも足さない。[設計](../design.md#cli-はプリミティブに保つ)）。保存（`save`）してから閉じる（`close`）
  と、[下記の道具](#図面を扱う道具documentssaveclose)で保存確認なしに閉じられます。
- **占有の中でだけ呼べます**（[通信規則「占有」](../protocol.md#占有セッション)。占有していなければ
  `no_session`、ほかが占有していれば `occupied` で断られる）。Vectorworks が終われば占有も消えます。
- **終了の依頼の最中は断られます**（`busy`。[通信規則「Vectorworks が処理中」](../protocol.md#vectorworks-が処理中)）。
  上の競合で保存の確認が開いている間に届いた 2 つ目の `quit` も `busy` で断られるので、終了の依頼は重なりません
  （この判定は残す。段 6 の前に、入れ子の判定にまとめるかを決める。[構成「Vectorworks が処理中」](architecture.md#vectorworks-が処理中)）。
- **再起動の手段は持ちません**（第 2 引数は常に偽）。起動し直すのは呼ぶ側で、`session start` で占有を
  取ってから `quit`（`--session` を付ける）・止まるのを待つ・`launch`・動き出すのを待つ、を組み合わせます（更新を
  挟む形を含めて[インストール「流れ」](install-and-update.md#流れ)）。
- 終了を見届けるのは呼ぶ側の役割です（`vw2026 status` を止まっていると答えるまで繰り返す。保存の確認を開いている間の扱いは
  [通信規則「生存の判定」](../protocol.md#生存の判定)）。

## 図面を扱う道具（`documents`・`save`・`close`）

`quit` の前に図面を保存して閉じるための道具です（[実装の順序](plan.md)の段 5）。使う SDK 呼び出しは SDK
リファレンスで確かめてあります（[Findings「指定した図面を保存する・閉じる」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#指定した図面を保存する閉じる前面に依存せず)。
VW 2026 / macOS。Windows も振る舞いは同じで、違うのはパスの比べ方。[同「Windows」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#windows振る舞いは-mac-と同じ違うのはパスの比べ方と開ける図面は-8-枚まで)・[SDK リファレンス #283](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/283)）。**変更を捨てて閉じる道具は持ちません**（人の変更を黙って捨てる手段を
作らない）。保存は呼ぶ側の責任で、保存してから閉じれば保存の確認は出ません。必要のない図面は、一時フォルダなどへ
保存してから閉じ、ファイルは利用者が自分で消します。

**前面の図面（コンテキスト）に依存せず、対象の図面を引数で指定します。** プラグインが自分で開いた図面かを
覚えることもしません（どの図面にも使える）。

| 道具 | 種類 | 引数 | 結果 |
| --- | --- | --- | --- |
| `documents` | read | なし | `{"documents":[{"name","path","untitled","active"}],"count"}` |
| `save` | app | `document`（必須）・`to`（省略可） | `{"document","path"}`（保存した後の図面のパス） |
| `close` | app | `document`（必須） | `{"closed":true}` |

- **図面が 1 枚も開いていなければ、`save`・`close` は `internal` で失敗し、`detail` に `{"reason":"no_document"}` を
  付けます**（[読む道具](#読む道具layersclasseslayer_objectsobject_counts)と同じ。`document` を確かめる前に返す）。
  `documents` は失敗にせず、空の一覧を返します。
- **`name`** は `path` の最後の区切り文字より後ろの部分です（SDK に名前を聞かずにパスから作る）。無題の図面は
  「名称未設定 N」になります。
- **`document`** は対象の図面のパスで、`documents` の `path` を渡します。パスは図面を一意に決めます
  （同じファイル名を別のフォルダに保存した 2 枚も見分けられる）。**無題の図面のパスは「アプリケーションの
  フォルダ＋名称未設定 N」**で、N はセッション全体の連番です（開いている間は重ならない。ファイルとしては
  存在しない）。`untitled` が真なら一度も保存していない図面です。開いている図面に一致しなければ
  `invalid_args` で断ります。
  - **`path` は正規化しません。** SDK が返した文字列をそのまま返します。Windows では、保存したときに渡された
    文字列がそのまま残り、区切りだけが `\` に揃います（ドライブ文字の大小・名前の大小・8.3 の短い名前は直らない）。
    同じファイルでも、保存したときの渡し方しだいで別の文字列になります。
- **`save`** は対象の図面を保存します。種類は `app` です（図面の中身は変えず、ファイルに書く。
  [ブリッジ「道具の表」](bridge.md#道具の表)）。`to` を省くと、その図面自身のパスへ上書き保存します（無題の図面は
  保存先が無いので `to` を要します。一時フォルダなどを指定する。省くと `invalid_args`）。
  - **`to` を渡せるのは無題の図面だけです。** 保存済みの図面に `to` を渡すと `invalid_args` で断ります。
    別名保存は図面のパスを保存先へ移す（「名前を付けて保存」と同じ）ので、利用者の図面に許すと、利用者が
    あとで ⌘S を押したときに一時フォルダのファイルへ保存されてしまうためです。無題の図面は元のファイルが
    無く、利用者の作業を移すことにならないので許します。
  - **結果の `path` は、保存のあとに `GetOpenFilesList` から読み直した文字列です**（`to` をそのまま返さない）。
    Windows では `/` が `\` に揃って残るので、`to` をそのまま `close` の `document` に渡すと一致しません。読み直すのは、
    保存の直後に前面になっている項目です（前面を戻す前に読む。`fileRef` が保存の前後で変わらないかには頼らない）。
  - **`to` に既にあるファイルを渡しても保存できません**（SDK が `GSError` 50331649 を返し、何も起きない。
    mac・Windows とも、どの形で渡しても）。図面自身のパスへの上書き保存（`to` を省く。下記）はこれに当たらず、
    次の確かめは `to` を渡したときだけ行います。**保存先が別の図面で開いているファイルなら、SDK が警告のダイアログを
    出して人が OK を押すまで戻りません**（Windows では、開いている図面のパスと文字列が完全に一致するときだけ出る）。
    そこで、呼ぶ前に次の順に確かめて `internal` で断ります。
    1. `to` が開いている図面のパスと一致する（比べ方は下記「パスの一致の判定」）。保存先が開いている図面だと
       示すため（`to_is_open`）、先に確かめます。
    2. `to` がディスクにある（ファイルシステムに聞く）。大文字と小文字・8.3 の短い名前・区切り文字の違いは
       ファイルシステムが吸収するので、文字列の比べ方に依らずに防げます。

    上書きしたければ、呼ぶ側が先にファイルを消します。**呼ぶ側が機械的に見分けられるよう、失敗の `detail` に
    `reason` を付けます:** `to_is_open`（保存先が開いている図面）・`file_exists`（保存先が既にある）。どちらも
    `internal` です（`invalid_args` と `internal` の分け方は[冒頭](#最初に提供する道具)）。
    確かめてから保存するまでの間にファイルができた場合は、SDK のエラーを `internal` で返します
    （開いている図面でなければダイアログは出ない）。Windows で 260 文字を超える保存先も、SDK が断るので `internal` に
    なります。どちらの `reason` も保存を呼ぶ前に返すことは、段 5 で無 SDK のテストで確かめます
    （[実装の順序](plan.md)の段 5）。
  - **`SaveActiveDocumentPath` が 0 以外を返したら、`internal` で失敗し、`detail` に
    `{"reason":"save_failed","gs_error":<値>}` を付けます。** `GSError` は SDK のエラーの番号（`Uint32`）で、値ごとの
    意味は SDK に書かれていません（実測で出たのは 50331649＝`0x03000001` だけで、保存先が既にあるときも長すぎるときも
    同じ値）。そのため理由を値で分けず、値をそのまま載せます。
  - **利用者が開いた図面の上書き保存がダイアログを出す場面（古い版のファイル・読み取り専用のファイルなど）と、`to` の
    拡張子・親のフォルダの扱いは確かめていません**（[SDK リファレンス #288](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/288)
    で調べている）。結果を見て、呼ぶ前に断るものを決めます。
  - **保存は、Vectorworks の取り消しの履歴に段を 1 つ積みます**（別名保存で確かめた。[Findings「Undo」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Undo.md)の
    「別名保存は取り消しの段を 1 つ積む」）。保存そのものは取り消せず、ファイルは保存したまま残ります。利用者が `save` の
    あとに取り消しを押すと 1 回目は何も戻らず、2 回目からそれまでの操作が戻ります。上書き保存でも積むかは
    [#288](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/288) で調べています。
  - 内部では `GetOpenFilesList` からパスの一致する項目の `fileRef` を引き、`SwitchToOpenFile` →
    `SaveActiveDocumentPath` で保存します。**`to` を省いたときは、`GetOpenFilesList` が返した文字列をそのまま
    保存先に渡します**（Windows では、大文字と小文字だけが違うパスを渡すと自分のファイルが「既にある」として
    断られ、上書き保存にならない）。**前面の図面とカレントレイヤは、保存の前後で元に戻せます**
    （戻すのも `SwitchToOpenFile`）。
- **パスの一致の判定。** `document` は `documents` が返した文字列と**完全に一致**するものを探します（呼ぶ側は
  `documents` の結果をそのまま渡す）。`to` は新しいパスで、開いている図面の保存先との衝突を**見逃さない側**に倒して
  比べます: 絶対パスを要し（相対パスは `invalid_args`）、区切り文字を揃え、Unicode を正規化（NFC。macOS のファイル名は
  NFD で返ることがある）し、**大文字と小文字を区別せず**に比べます。大文字と小文字だけが違う別のファイルも断る
  ことになりますが、警告のダイアログが出るより安全です（ダイアログが出るのは文字列が完全に一致するときだけなので、
  緩く比べて断れば必ずその手前で止まる）。ディスクにあるファイルはこの比べ方によらず 2 つ目の確かめで断るので、
  この比べ方は、開いている図面との衝突を `to_is_open` として示すためのものです（8.3 の短い名前と長い名前の違いは
  ここでは見分けず、`file_exists` で断る）。
- **`close`** は対象の図面を閉じます。**保存はしません。** 種類は `app` です。
  対象の図面を `SwitchToOpenFile` で前面にしてから `DoMenuName("Close", 0)` を呼びます（メニューの「閉じる」と
  同じで、前面の図面に効く。`CloseDocument()` は確認なしで変更を捨てるので使いません）。
  - **保存した直後の図面は、確認なしに閉じます。** 確認が出るのは、無題の図面（無変更でも）、
    `OpenDocumentPath` で開いただけの図面（何もしなくても。開いた時点で「変更あり」になる。同じパスへ
    `save` してから閉じれば出ない）、`MarkCurrentDocChanged()` した図面です。
  - **確認が出ると、人が答えるまで呼び出しから戻りません。** その間の受け付けは OS で違います
    （[Findings「Timers and Notifications」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Timers%20and%20Notifications.md) の 4 の 9・
    [Findings「Documents」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#windows振る舞いは-mac-と同じ違うのはパスの比べ方と開ける図面は-8-枚まで)）。
    - **Windows** では、`close` を呼んだタイマー自身が確認の最中に入り直します。その受け付けは入れ子になるので、
      要求をすべて `busy` で断ります（[構成「Vectorworks が処理中」](architecture.md#vectorworks-が処理中)）。
    - **mac** では、タイマーは入り直しません（`CFRunLoopTimer` は自分の処理の最中に同じタイマーを呼ばない）。
      このプラグインのタイマーは 1 本なので、確認の間は何も応えません（`ping` も）。要求はスプールに残り、
      呼ぶ側は待つ上限を過ぎて終わります（[通信規則「生存の判定」](../protocol.md#生存の判定)）。
  - **戻り値（常に 0）では成否が分かりません。** 呼び出しの後に `GetOpenFilesList` を読み、対象の図面が
    残っていれば `internal` で失敗し、`detail` に `{"reason":"not_closed"}` を付けます。残ったのが確認の取り消しの
    ためかは SDK から読めないので（実測で残ったのは取り消したときだけ）、理由の綴りは「閉じなかった」にします。
    確認で人が「保存しない」を選べば図面は閉じ、成功になります。
  - 閉じたあとに前面になるのは別の図面です。`close` の前の前面の図面が残っていれば、`SwitchToOpenFile` で
    戻します（前面を閉じたときは戻せない）。
- **undo イベントの外で足された図形は「変更あり」にならず、`close` で確認なしに黙って捨てられます。**
  VW の仕様で、プラグインからは検出も防止もできません（「確認が出ないから変更は無い」とは読めない）。
  **この危険を認めたうえで `close` を持つ、と決めます。** 利用者の図面を保存せずに閉じる道具を持たないという
  規則（CLAUDE.md の 5）は、変更を意図して捨てる手段を作らないことを指し、`close` は捨てる手段ではありません
  （変更が分かれば保存の確認が出る）。このプラグインの図面に書く道具は undo の記録の中で書くので起きません。
  ほかのプラグインやスクリプトが記録の外で書いた変更は、呼ぶ側が `close` の前に `save` します。
- 図面を開く道具は[設計「決めたこと」](../design.md#決めたこと)の「対象の図面」にあります（未実装）。
  `OpenDocumentPath` で開いた図面は、`save` してから `close` します。

## 他のプラグインの機能

手段は `CallPluginLibrary` です（[設計「他のプラグインの道具」](../design.md#他のプラグインの道具)）。
道具の形は[未決 3](open-questions.md#3-ほかのプラグインの機能の呼び出しの形)で決めます。
