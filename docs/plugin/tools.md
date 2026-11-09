# 最初に提供する道具

図面を**読むだけ**の道具と、図面を保存して閉じる道具（`documents`・`save`・`close`）、Vectorworks を終わらせる 1 つ（`quit`）から始めます。最初の
リリース（[実装の順序](plan.md)の段 2）に載せるのは `ping` だけで、共通部の変更は段 4、読む道具と図面を扱う道具は段 5、`quit` は段 6 で
足します。更新は道具ではなく CLI の `vw2026 install` です（[インストール](install-and-update.md#インストールと更新vw2026-install)）。**SDK 呼び出しは、SDK リファレンスで
挙動が確かめられているものだけを使います**（無いものはリファレンスで調べてから足す。[SDK の調査](../sdk-research.md)）。

引数は、省略できるものに既定値を設けます（表の「既定」）。知らない引数と、知っている引数の
未知の値（型の違い・決まった綴り以外の値）は `invalid_args` で返します（[通信規則「互換性」](../protocol.md#互換性)）。

**数は、整数と実数を書き方で区別しません**（JSON に区別が無く、`2.0` を `2` と書き出す言語も多いため）。
実数の引数はどの書き方の数も受けます。整数の引数は、値が整数なら書き方によらず受け（`2`・`2.0`・`2e0`）、
小数部のある値（`2.5`）と、範囲の外の値は `invalid_args` で断ります。数でない値（文字列の `"2"` など）は
型の違いとして断ります。**整数の引数は、道具ごとに範囲を決めていなくても 0 以上 2147483647（`int32` の最大）以下です**
（`offset:1e30` のように整数でも収まらない値を断るため）。
- **整数かどうかと範囲は、書かれた 10 進の値で正確に決めます**（浮動小数点数に直してから決めない）。`2.0000000000000001` は
  小数部があるので断り、`1e400` は範囲の外として断ります。浮動小数点数に直すと、前者は 2 に丸まって受けてしまい、後者は
  無限大になるためです。
  - `-0` と `-0.0` は値が 0 なので、0 として受けます（負の値として断らない）。`0e99999999`・`0e-99999999` も値は 0 なので
    受けます。
  - **範囲の外か・小数部があるかは、指数だけでなく、仮数の最初の 0 でない数字の位置に指数を足した実際の桁の位置で
    決めます。** `0.0000000001e10` も、1 の後に 0 を 1 万個並べて `e-10000` としたものも 1 なので受けます。
  - 指数は先頭の 0 を読み飛ばしてから数えます（JSON の文法は指数の先頭の 0 を許す）。`2e0000000000000000000000` は 2 です。
  - **指数は `int64` で数え、±2^31 で飽和させてよい。** 要求は 1 MiB までなので、仮数の桁の位置は ±2^20 程度に
    収まり、指数の大きさがそれを十分に超えたら、仮数が 0 でない限り答えは決まるためです（正の向きは範囲の外、負の
    向きは小数部がある）。飽和させる境を `int64` の端（`INT64_MAX`・`INT64_MIN`）にしないのは、そのあとで仮数の桁の
    位置を足すと符号付き整数が溢れる（未定義の動作）ためです。`1e999999999999999999` は範囲の外、`1e-999999999999999999` は小数部があるとして、
    どちらも溢れたり長く掛かったりせずに `invalid_args` で断ります。
- `core::Json::asInt64` は綴り（小数部と指数部が無いか）で整数かを決めるので、段 5 で値で決める読み方を足します。

**結果の実数（`scale`・`bounds` の値）の桁は、元の値に戻る最も短いものにし、書き方は Go の `encoding/json`
に合わせます**（プラグインの綴りを決まった基準に揃え、その基準を見本で C++ と Go の両側から確かめるため）。
- 桁は `std::to_chars` の精度を指定しない形で求めます。ただし `to_chars` の書き方はそのまま使いません（固定小数点より
  指数のほうが短ければ指数を選ぶので、`100000` が `1e+05`、`0.0001` が `1e-04` になる）。
- **0 と、`1e-6 ≤ |x| < 1e21` は固定小数点で書きます**（`0`・`100000`・`1500000`・`0.0001`・`0.000001`・`2366.025`）。整数の値は
  小数点を付けません（`50.0` は `50`）。上と同じく、呼ぶ側は書き方で整数と実数を区別しません。
- それ以外は指数で書き、指数は符号を付けて先頭の 0 を付けません（`1e+21`・`1e-7`・`1.5e-7`）。
- `-0` も Go と同じく `-0` と書きます（読み手には 0 と同じ値。Go と綴りを揃えるため、符号を落とさない）。
- **実数と綴りの組は見本のファイルに置き**、C++ のテストは書き出しの口と、Go のテストは `json.Marshal` と照らします
  （段 5 で足す）。Go と同じ綴りであることを、片側のテストに書いた期待だけで決めないためです。これは道具の結果の
  決まりで通信規則ではないので、見本は通信規則の見本（`protocol/fixtures`。[通信規則の見本](../fixtures.md)）とは
  分けて `tests/fixtures/` に置きます。
- **有限でない値（無限大・NaN）は JSON に書けないので、そのフィールドを `null` にします**（`bounds` は 4 つの
  どれかが有限でなければ `bounds` ごと `null`）。この決まりは道具ごとに書かず、`core` の 1 か所に置きます:
  `core::Json` の実数の口は「有限でなければ `null` を返す」1 つだけにし、道具の結果の実数はどれもそれで作ります。`bounds` は
  `GetObjectBounds` の戻り値（真偽）と外形の 4 つの値を受けて `bounds`（または `null`）を返す 1 つの関数で作ります
  （偽の場合も無 SDK のテストで確かめられるように）。
- `core::Json` は数を綴りで持ち、実数を書き出す口が無いので、段 5 で足します。

**引数の確かめは次の順に行い、最初に当たったもので返します**（どの `reason` を返すかを決めておくため）。
1. 引数の形（知らない引数・型・値の範囲・必須の欠け・`save` の `to` の形）→ `invalid_args`。
2. 図面が無い → `internal`（`no_document`）。**「図面が無い」は、道具が使うものの有無で決めます。** 前面の図面を読む
   道具（`layers`・`classes`・`layer_objects`・`object_counts`）は `GetCurrentLayer()` が `nullptr` か（`ping` の
   `document_open` と同じ基準。`layers` は `current` のためにどのみち呼ぶ）、図面を名指す道具（`save`・`close`）は
   `GetOpenFilesList` の件数が 0 か（名指す図面を引く一覧と同じもの）で決めます。2 つが一致することを確かめたのは、
   起動の直後と全部閉じた後だけです（[Findings「Timers and Notifications」の 4 の 8](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Timers%20and%20Notifications.md)）。件数が 1 以上でも前面の
   図面が無い状態があれば、読む道具は `no_document` で断り、`save`・`close` は名指した図面に働きます（どちらも道具が
   使うものに合っている）。`documents` は失敗にせず空の一覧を返し、`ping` は失敗せずに `document_open` を偽にします。
3. 引数が名指すもの（`document`・`layer`）が実在しない → `invalid_args`。名指す図面が 1 つに決まらない
   （`document` に一致する図面が 2 枚以上）→ `internal`（`document_ambiguous`）。
4. 道具ごとの状態（`save` は下記の順）→ `internal`。

**道具が返す失敗の `invalid_args` と `internal` は、次のように分けます**（[通信規則「応答」](../protocol.md#応答idresjson)）。

- **`invalid_args`** は、引数そのものが誤っているとき: 知らない引数・型や値の誤り・必須の欠け、引数が名指すもの
  （`document` の図面・`layer` のレイヤ）が実在しない、
  `save` の `to` の形（拡張子・使えない文字など）。
- **`internal`** は、引数は正しく、名指すものも実在するが、それ以外の状態のために実行できないとき: 図面が無い
  （`no_document`）・名指す図面が 1 つに決まらない（`save`・`close` の `document_ambiguous`）・
  図面が開いている（`quit` の `documents_open`）・保存先が埋まっている（`save` の `to_is_open`・
  `file_exists`。`to` は新しく作るファイルを名指すので、既にあることは状態に当たる）・保存先の親のフォルダが無い
  （`parent_missing`）・保存先を確かめられない（`to_check_failed`）・図面が閉じなかった（`close` の
  `not_closed`）・SDK の失敗（`save` の `save_failed`）・道具の中の例外。

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
| `save` | app | `document`（必須）・`to`（必須） | 同上 |
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
`GetOpenFilesList()` の件数と、起動直後も全部閉じた後も一致しました（確かめたのはこの 2 つの状態。mac / Windows。
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

- **図面が開かれていなければ（`GetCurrentLayer()` が `nullptr` か。[冒頭](#最初に提供する道具)の 2）`internal` で失敗し、`detail` に `{"reason":"no_document"}` を付けます。**
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
| `scale` | 縮尺の分母（`GetLayerScaleN`。1/50 なら `50`、1:1 なら `1`）。SDK は `double_gs` で返すので実数として書きます（[Findings「レイヤの縮尺」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Layers%20and%20Stories.md#レイヤの縮尺setlayerscalen-は黙って効かないことがある)。確かめた縮尺はどれも整数で、整数でない縮尺は確かめていない）。**シートレイヤは SDK の返すとおり常に `1`**。有限でなければ `null`（[冒頭](#最初に提供する道具)） |
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
- **`index`** は、絞ったあとの並び（背面→前面。型 0 と 90 を除く）の中の 0 からの位置です（`offset` に、返した
  配列の中の位置を足したもの）。消去・取り消し・並べ替えで変わるので、図形の識別子には使えません。レイヤ全体の
  前後関係が要るなら、`type` を省いて取ります。
- **`limit` は 1〜1000、`offset` は 0 以上、`type` は 0 以上の整数です。** 範囲の外と、小数部のある値（`2.5`）は
  `invalid_args` で断ります（数の受け方は[冒頭](#最初に提供する道具)）。`offset` が `total` 以上なら `objects` は空で
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
  単位の取り違えを防ぐために置きます。値は図面の単位にもレイヤの縮尺にもよらない世界座標の実数です。`GetObjectBounds` が偽を
  返した図形と、4 つの値のどれかが有限でない図形は `null` です（[冒頭](#最初に提供する道具)）。偽を返すと確かめたのは
  undo の置き石だけで（[Findings「Layer Objects」の 2](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Layer%20Objects.md#2-列に混ざる図形でないもの)）、置き石は数える図形から除いてあるので、
  列に残る図形で偽を返すものは確かめていません。返したときに備えて `null` にします。
  **有限かは、下の最小と最大をとる前に SDK の外形の 4 つの値で確かめます**（`std::min(a, b)` は `b` が NaN だと
  `a` を返すので、先に最小と最大をとると NaN が落ちて有限の値の `bounds` になる）。
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
`outSaved` も `fIsInMemoryOnly` も、図面がディスクのファイルに結び付いているかを表すだけ）、無題の図面は変更が無くても
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
  | `documents[].path` | 図面のパス（`documents` と同じ。`untitled` が真の図面は「アプリケーションのフォルダ＋名前」（「名称未設定 N」・「<元の名前> コピー 1」など）でファイルとしては存在しない） |
  | `documents[].untitled` | ディスクのファイルに結び付いていない図面か（`documents` の `untitled` と同じ） |
- **図面が 0 枚なら、保存の確認は出ません。** 応答を書いてから、受け付けの外で
  `gSDK->CloseAllFilesAndQuitVectorworks(true, false)` を頼みます（[構成「受け付けの流れ」](architecture.md#受け付けの流れ)）。
  第 1 引数は常に真です（`quit` は図面を保存するか捨てるかを決めない。決めるのは呼ぶ側で、`save`（新しいパスへ）・`close`（変更を捨てる）で先に片付ける）。確かめてから頼むまでの間に利用者が
  図面を開くと、保存の確認が出うる競合が残りますが、`quit` は占有の中でだけ呼べ、呼ぶ側が図面を閉じた直後に
  呼ぶ使い方なので、通常は起きません。
- **図面を保存して閉じるのは呼ぶ側です。** 保存するか捨てるかも呼ぶ側が決めます（`quit` に保存する・捨てる指定は持たず、
  CLI にも足さない。[設計](../design.md#cli-はプリミティブに保つ)）。残したい図面（変更）は `save` で新しいパスへ保存してから、
  どの図面も[下記の `close`](#図面を扱う道具documentssaveclose)で閉じます。`close` は保存の確認を出さず、未保存の変更を捨てます。
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

`quit` の前に図面を片付ける（残したい図面を新しいパスへ保存する・図面を閉じる）ための道具です（[実装の順序](plan.md)の段 5）。使う SDK 呼び出しは SDK
リファレンスで確かめてあります（[Findings「指定した図面を保存する・閉じる」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#指定した図面を保存する閉じる前面に依存せず)。
VW 2026 / macOS。Windows も振る舞いは同じで、違うのはパスの比べ方。[同「Windows」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#windows振る舞いは-mac-と同じ違うのはパスの比べ方と開ける図面は-8-枚まで)・[SDK リファレンス #283](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/283)）。**`close` は保存の確認を出さずに閉じ、未保存の変更を捨てます**（利用者の判断。下記）。保存は呼ぶ側の責任で、
残したい図面は、`save` で新しいパスへ保存してから `close` します（`save` は上書き保存をしない）。要らない図面は、保存せずに `close` します。
利用者の環境設定によっては、保存するたびに保存先の隣の `VW Backup/` に図面と同じ大きさのコピーもできます
（大きな図面を繰り返し保存する使い方ではディスクを食います）。

**前面の図面（コンテキスト）に依存せず、対象の図面を引数で指定します。** プラグインが自分で開いた図面かを
覚えることもしません（どの図面にも使える）。

| 道具 | 種類 | 引数 | 結果 |
| --- | --- | --- | --- |
| `documents` | read | なし | `{"documents":[{"name","path","untitled","active"}],"count"}` |
| `save` | app | `document`（必須）・`to`（必須） | `{"document","path"}`（下記） |
| `close` | app | `document`（必須） | `{"closed":true}` |

```json
{"documents":[{"name":"plan.vwx","path":"/Users/me/plan.vwx","untitled":false,"active":true},{"name":"名称未設定 1","path":"/Applications/Vectorworks 2026/名称未設定 1","untitled":true,"active":false}],"count":2}
{"document":"/Applications/Vectorworks 2026/名称未設定 1","path":"/Users/me/tmp/a.vwx"}
{"closed":true}
```

上から `documents`・無題の図面を `to` へ保存した `save`・`close` の結果です。

- **図面が 1 枚も開いていなければ、`save`・`close` は `internal` で失敗し、`detail` に `{"reason":"no_document"}` を
  付けます**（`GetOpenFilesList` の件数で決める。[冒頭](#最初に提供する道具)の 2。`document` を確かめる前に返す）。
  `documents` は失敗にせず、空の一覧を返します。
- **`documents`** は `GetOpenFilesList` の項目を、その順のまま並べます（並べ替えない）。**`count`** はその数です。
  順の意味（開いた順・前面からの順など）は確かめていないので、呼ぶ側は順に頼らず `active` と `path` で見分けます。
- **`name`** は `path` の最後の区切り文字より後ろの部分です（SDK に名前を聞かずにパスから作る）。
- **`active`** は、前面の図面（読む道具が読む図面）かを表します。`GetOpenFilesList` の `fIsActive` をそのまま
  載せます（構造体は [Findings「存在する API」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#存在する-apiヘッダ根拠)、実測の `active` は [同「`GetOpenFilesList` と `fileRef`」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#getopenfileslist-と-fileref)）。真がちょうど 1 つかは
  確かめていません。
- **`untitled`** は、図面がディスクのファイルに結び付いていないか（`GetOpenFilesList` の `fIsInMemoryOnly`）です。
  一度も保存していない図面のほか、**古い版・読み取り専用・ほかの Vectorworks が使用中のファイルを開いた図面**も
  真です（Vectorworks はこれらを元のファイルに結び付かないコピーとして開く。`OpenDocumentPath` で開いても確認なしに
  コピーになる。[Findings「利用者の図面を上書き保存する」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#利用者の図面を上書き保存するダイアログが出る場面と保存先の文字列288)）。
  **Windows で確かめたのは読み取り専用と使用中のファイルで、どちらも mac と同じく `untitled` が真です**（使用中は、
  別のプロセスがファイルを書き込みで握った状態で代わりに測った。`OpenDocumentPath` は確認なしにコピーで開き、戻り値は
  `true`。[Findings「Windows——ほかの Vectorworks が開くファイル」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#windowsほかの-vectorworks-が開くファイルと測れなかった残り291)。#291）。
  **Windows の古い版のファイルと、2 つ目の Vectorworks が開いているファイルは測れていません**（調べた環境では古い版の
  ファイルを作れず、2 つ目の Vectorworks を起動できなかった）。`untitled` は `fIsInMemoryOnly` をそのまま返すので、
  プラグインはこれらの場面でも Vectorworks の答えに従います。古い版のファイルが Windows で `untitled` にならなくても、
  `save` は新しいパスへしか保存しない（下記）ので、`save` が利用者の古い版のファイルを置き換えることはありません。`close` も保存せずに閉じるので、置き換わりません。
  段 5 の実機の確認で、mac で作った古い版のファイルを Windows で開いて `untitled` を見ます。
  **`untitled` が真の図面の `path` は「アプリケーションのフォルダ＋名前」**で、ファイルとしては存在しません（名前は
  「名称未設定 N」・「<元の名前> コピー 1」・「<元の名前> v2026.vwx」など。N はセッション全体の連番で、開いている間は
  重ならない）。**`untitled` が真の図面は、`path` がほかの図面と重なることがあります**（下記。その図面は `save`・
  `close` で名指せない）。呼ぶ側は `path` を鍵にして一覧を扱うと項目を取りこぼします。
- **`document`** は対象の図面のパスで、`documents` の `path` を渡します。ディスクのファイルに結び付いた図面どうしは、
  パスで一意に決まります（同じファイル名を別のフォルダに保存した 2 枚も見分けられる）。開いている図面に一致しなければ
  `invalid_args` で断ります。
  - **`untitled` が真の図面のパスは、一意とは限りません**（確かめていない。Findings も、無題の図面を指すなら
    `fileRef` のほうが確かだとしている。[Findings「指定した図面を保存する・閉じる」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#指定した図面を保存する閉じる前面に依存せず)）。
    Vectorworks がアプリケーションのフォルダと名前から作るので、別々のフォルダにある同じ名前の古い版のファイルを開いた
    2 枚は、同じ `<アプリ>/<名前> v2026.vwx` になりえます（「名称未設定 N」の N は重ならない）。利用者がアプリケーションの
    フォルダに同じ名前のファイルを保存して開いていれば、`untitled` が偽の図面とも重なりえます。**`untitled` によらず、
    一致する図面が 2 枚以上あれば、どれも保存・閉じずに `internal` で失敗し、`detail` に `{"reason":"document_ambiguous"}`
    を付けます**（名指すものは実在するので `invalid_args` にしない。[冒頭](#最初に提供する道具)の 3 で、名指すものの
    実在の直後に確かめる）。黙ってどれか 1 枚に効かせると、呼ぶ側の思っていない図面を閉じうるためです。
    呼ぶ側は、利用者に一方を閉じてもらうなどして見分けられるようにします。
  - **`path` は正規化しません。** SDK が返した文字列をそのまま返します。Windows では、保存したときに渡された
    文字列がそのまま残り、区切りだけが `\` に揃います（ドライブ文字の大小・名前の大小・8.3 の短い名前は直らない）。
    同じファイルでも、保存したときの渡し方しだいで別の文字列になります。
- **`save`** は、名指した図面を `to` の新しいパスへ保存します。種類は `app` です（図面の中身は変えず、ファイルに書く。
  [ブリッジ「道具の表」](bridge.md#道具の表)）。`to` は必須で（省くと `invalid_args`）、既にあるファイルは断ります（下記）。
  `untitled` によらず、どの図面にも使え、同じ図面に何度でも使えます（そのたびに新しいファイルができる）。
  - **上書き保存（図面自身のパスへの保存）は提供しません。** 上書き保存は、自分のファイルが移された・消された、開いた後で
    読み取り専用になったなどの場面で警告のダイアログを出し、人が押すまで戻りません
    （[Findings「利用者の図面を上書き保存する」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#利用者の図面を上書き保存するダイアログが出る場面と保存先の文字列288)。#288）。
    前もって確かめて断るには、OS ごとに書き込めるかを判定する手順が要り（読み取り専用の属性・ロック・ACL・リンク・
    ネットワークの失敗）、それでも確かめきれない場面が残るので、提供しないことで避けます。図面自身のパスは開いている図面の
    パスなので、`to` に渡すと `to_is_open` で断られます（下記）。要るようになれば、`to` を省いたときの上書き保存を足せます
    （いまの呼び方は変わらない）。
  - **保存すると、図面のパスは `to` へ移ります**（「名前を付けて保存」と同じ）。元のファイルは書き換わりません。
    そのあとは、利用者の ⌘S もダイアログを出さずに `to` のファイルへ保存します。利用者が開いた図面を名指したときも
    同じで、利用者の ⌘S の先が呼ぶ側の保存先に変わります。古い版・読み取り専用・使用中のファイルを開いたコピーは
    見た目が利用者のファイルそのものなので、利用者が気付きにくくなります。**呼ぶ側が名指した図面なので、これを認めます**
    （利用者の判断。CLAUDE.md の 5。`close` が変更を捨てるのと同じ扱い）。呼ぶ側は、利用者が開いている図面を `save` したら、
    その図面を閉じるまでを 1 続きの操作にします。
  - **同じ図面を続けて保存するときは、毎回新しい `to` を渡します**（直前の保存先は開いている図面のパスなので、渡すと
    `to_is_open`）。古いファイルは残り、プラグインも CLI も消しません（CLAUDE.md の 9）。
  - **`untitled` が真の図面の `path` はアプリケーションのフォルダを指し、ファイルとしては存在しません**
    （[Findings「利用者の図面を上書き保存する」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#利用者の図面を上書き保存するダイアログが出る場面と保存先の文字列288)）。そのまま保存先に使えないので、ほかの図面と同じく `to` を要します。
  - **保存先のフォルダにファイルを足せないときは、Vectorworks がダイアログを出さずに断ります**（`save_failed`。書き込みの拒否・
    読み取り専用のボリュームや共有。mac も Windows も。[Findings「新しいパスへの保存で、親のフォルダに書き込めないとき」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#新しいパスへの保存で親のフォルダに書き込めないとき304)。#304）。プラグインは前もって確かめません。
  - **ファイルは足せるが、保存先のフォルダに Vectorworks のバックアップのフォルダ（`VW Backup`）を作れないときは、保存は通るのに
    OK だけの警告（「バックアップフォルダを作成することができませんでした」）が出て、押すまで戻りません**（保存のたびに出る。
    サブフォルダの作成だけが拒まれている、同じ名前のファイルがある、など）。**プラグインは前もって確かめず、この警告を認めます**
    （利用者の判断）。バックアップを作るか・どこへ作るかは利用者が環境設定（自動保存）で変えられるので、プラグインが
    判断すると設定と食い違うためです。通常の保存先では起きません。出たときは、呼ぶ側がダイアログのコマンドで OK を押します
    （[SDK リファレンス #302](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/302) の結果で決める。それまでは人が押す）。警告が出ても `GSError` は 0 で、保存は成功として返ります。
    - **上書き保存をやめた理由（警告のダイアログで止まる）との違い:** 上書き保存の警告は、利用者が図面のファイルを
      動かした・読み取り専用にしたなど、ふつうに起きうる場面で出て、保存も通らないことがありました。この警告は保存は通り、
      バックアップのフォルダを作れない特別な保存先でだけ出て、出るかどうかは利用者の環境設定で変わります。
    - **警告が出ている間は、ダイアログを出す道具と同じく人が答えるまで戻りません**（[通信規則「予約された道具」](../protocol.md#予約された道具)の「長くかかる道具」）。 Windows では、呼んだタイマー自身が入り直して
      受け付けが入れ子になり、ほかの要求はすべて `busy` で断られます。mac ではタイマーが入り直さず、何も応えません
      （[構成「Vectorworks が処理中」](architecture.md#vectorworks-が処理中)）。`save` を呼んだ `vw2026 call` は、既定の待つ上限
      （30 秒）を過ぎると「実行されたかもしれない」（終了コード 4）で終わり、応答を読めません。そうした保存先に保存しうる
      ときは長い `--timeout` を付けるか、警告が閉じる（ほかの要求が `busy`・無応答でなくなる）のを待ってから `documents` で
      保存されたかを確かめます（警告が開いている間は `documents` も断られるか応えないので、「保存されなかった」と読まない）。
  - **結果の `document`** は、引数の `document` をそのまま返したもの（保存する前の図面のパス）です。
  - **結果の `path` は、保存のあとに `GetOpenFilesList` から読み直した文字列です**（`to` をそのまま返さない。渡した文字列がそのまま
    一覧に残る。[Findings「Windows」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#windows振る舞いは-mac-と同じ違うのはパスの比べ方と開ける図面は-8-枚まで)・
    [同「すでに開かれています」の比べ方（mac）](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#このファイルはすでに開かれていますの比べ方macwindows-と同じ文字列の完全一致)）。
    読み直すのは、保存の直後に前面になっている項目です（前面を戻す前に読む。`fileRef` が保存の前後で変わらないかには頼らない）。
  - **`to` の形は、呼ぶ前に確かめて `invalid_args` で断ります。** Vectorworks は形を確かめずにその名前で保存し、
    成功と見分けが付かないものがあるためです（#288）。
    確かめは `src/core` に置き、**mac の規則と Windows の規則を別々の関数にして、どちらもどの OS でもビルドします。**
    中身は文字列の処理だけで OS の呼び出しを使わないので、OS で分ける必要がありません。**プラグインがどちらを使うかは
    `_WIN32` で選び、選ぶのはこの 1 か所だけです**（出荷するプラグインは OS ごとのビルドなので、各 OS のプラグインの
    規則はビルドで決まる）。テストは、どの OS の CI でも両方の関数を名指しして呼びます。そのため Windows の規則にも、
    ASan・UBSan（macOS のテストのジョブと ubuntu の fuzz のジョブ）と clang-tidy（ubuntu）が掛かります。
    比べ方は、下の「パスの一致の判定」のとおり引数で受け取ります。
    - **Windows では `/` を含む `to` を断ります**（直して渡さない）。Vectorworks に `/` 区切りのまま渡すと、別の名前で
      黙って保存されます（[Findings「Windows——測れた範囲」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#windows測れた範囲288)。#288）。呼ぶ側が `\` 区切りで渡します。
      プラグインはパスを書き換えないので、渡した文字列のとおりに保存されます。
    - 絶対パスでない・区切りで終わる（フォルダを指す）。**受ける絶対パスは、mac は `/` から始まるもの、Windows は
      `X:\`（ドライブ文字・`:`・`\`。ドライブ文字は ASCII の `A`〜`Z`・`a`〜`z` で、ロケールに依る判定をしない）で始まる
      ものと、UNC（`\\<サーバー>\<共有>\…`）です。** UNC はサーバー・共有・その下の 1 段以上（ファイル名）を要します
      （`\\<サーバー>\<共有>\<名前>.vwx` が最小。`\\server\a.vwx` と `\\server\share` は断る）。Vectorworks は UNC へ
      保存でき、渡した文字列がそのまま図面のパスになります（通常の共有へ保存できるかは段 5 の実機の確認で見る。[Findings「保存先のパスの形」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#保存先のパスの形unc相対-と-294)。#294）。
      Windows のほかの形——`\\?\`（保存されない）・`\\.\`（図面のパスにデバイスの名前空間が残る）・ドライブ相対の
      `C:a.vwx`・ルート相対の `\a.vwx`（保存されない）——は断ります（どれもダイアログは出ない。#294）。形ごとの判定は
      持たず、`\\?\` と `\\.\` はサーバーの名前の段が下の「使えない文字」（`?`）と「`.` の段」の確かめで、ドライブ相対と
      ルート相対は「`X:\` でも `\\` でも始まらない」で断られます。
      UNC の綴りと別の綴りで同じファイルを指す場合の比較は、下の「パスの一致の判定」に書きます。
    - `.`・`..` の段、空の段を含む（mac も Windows も）。空の段は、区切りが 2 つ以上続くところで、先頭も含めて断ります
      （mac の `//a.vwx`・`/x//a.vwx`、Windows の `C:\x\\a.vwx`。Windows の UNC の先頭の `\\` はちょうど 2 つだけを除き、3 つ目からは空の段として断る——
      `\\\server\share\a.vwx` も断る）。Vectorworks はこれらを字面で処理し（`..` と `//` を畳み、`.` を残す）、`x/./../a.vwx` は渡したパスと
      別のフォルダへ黙って保存されます。`.` の段を含むパスは図面のパスに残って、開いている図面との比較から外れます。
      `//` は同じフォルダへ畳まれますが、渡した文字列と図面のパスが食い違い、比べ方が単純でなくなるので、同じく断ります
      （#294）。
      **プラグインは正規化して渡さず、断ります。** これは Findings の勧め（[「CLI の `save` への帰結（パスの形）」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#cli-の-save-への帰結パスの形294):
      `to` は字面で正規化してから渡す）と違う決めです。字面で畳むとシンボリックリンクを段に含むパスで OS の解決とずれるので、
      正規化は呼ぶ側（自分の OS でパスを解決できる）に任せます。
    - Windows では、途中の段（フォルダの名前）の末尾が `.` か空白のもの（`C:\x.\a.vwx`・`C:\x..\a.vwx`・`C:\x \a.vwx`）を含む。
      Vectorworks は文字列を直さず OS に渡すので、`x.\` は Win32 の正規化で `x\` に入るのに図面のパスは `x.\…` のまま残り、
      `.` の段と同じく開いている図面との比較から外れます。UNC ではサーバーの名前の段にも掛けます（末尾に `.` の付いた完全修飾のホスト名 `\\fs.example.\share\a.vwx` も断る。
      `\\fs.\…` と `\\fs\…` が同じファイルを指して開いている図面との比較から外れるのを避けるため。呼ぶ側は末尾の `.` を外して渡す）。`x..\`・`x \` は実在する別のフォルダを指しえます
      （[Findings「途中の段の末尾が `.` か空白の保存先」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#途中の段の末尾が--か空白の保存先windows299)。#299）。**正規化して渡さず、断ります。**
      `GetFullPathNameW` では見分けられない（`x..\`・`x \` を変えない）ので、段の末尾の文字を字面で見ます。`.`・`..` の段は
      上の確かめで断られるので、ここの対象はそれ以外の段です。ファイル名の末尾は下の拡張子の確かめで断られます。
    - 拡張子が `.vwx` でない（大文字と小文字は区別しない）。Vectorworks は拡張子を足さず、`.txt`・`.dwg` でも中身は
      Vectorworks の形式のまま、その名前で保存します。末尾が `.` の名前もここで断ります（Windows は点を落とした
      ファイルを作り、図面のパスと食い違う）。
    - Windows では、先頭（ドライブ名の `X:`、UNC の `\\`）の後のどの段にも——UNC のサーバーと共有の段を含めて——`< > : " | ? *` を含む（`GSError` が 0 のまま `%3F` などに化けた名前で
      保存される）。
    - Windows では、255 文字（UTF-16 の単位）を超える段か、全体が 260 文字以上の `to`（段の上限はファイルシステムの名前の長さ。全体の境目は `MAX_PATH`）。SDK はこれを保存できず、ファイルシステムの確かめも長いパスでは
      「無い」と読み違えうる（3 が返ると、親のフォルダがあっても `parent_missing` になる）ので、確かめの前に断ります
      （[Findings「Windows」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#windows振る舞いは-mac-と同じ違うのはパスの比べ方と開ける図面は-8-枚まで)）。
    - mac では、255 バイト（UTF-8）を超える段か、全体が 1024 バイト（`PATH_MAX`）以上の `to`。ファイルシステムの確かめが
      `ENAMETOOLONG` で失敗し、`to_check_failed`（状態の失敗）に見えてしまうので、引数の誤りとして確かめの前に断ります。
    - 制御文字（U+0000〜U+001F）を含む（mac も Windows も）。SDK がどう振る舞うかは確かめていないので、確かめずに断ります。
      Windows の予約された名前（`CON.vwx` など）は、SDK がふつうに保存したので断りません
      （[Findings「Windows——測れた範囲」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#windows測れた範囲288)。`CON` 以外は確かめていない）。
  - **ネットワークの場所（ネットワークのドライブ・共有）の保存先では、サーバーが届かないと Vectorworks が止まります。**
    `save` の状態の確かめ（`file_exists`・`parent_missing`）と保存
    そのものはメインスレッドで行うので、サーバーが止まっている・ネットワークが切れているときは、OS が失敗を返すまで
    待ち、その間は画面の操作にも CLI のほかの要求にも応えません。段 5 では扱わず、この振る舞いを認めます。
    - **待ったあとの失敗は「無い」と分けます。** 「無い」に数えるのは、mac は `ENOENT`・`ENOTDIR`、Windows は
      2 `ERROR_FILE_NOT_FOUND`・3 `ERROR_PATH_NOT_FOUND`・67 `ERROR_BAD_NET_NAME`（サーバーに共有が無い）・
      267 `ERROR_DIRECTORY`（途中の段がファイル）だけです。
      `to` の確かめで OS の呼び出しがそれ以外の理由（ネットワークの失敗など。Windows の 53 `ERROR_BAD_NETPATH`・
      64 `ERROR_NETNAME_DELETED` など）で失敗したときは、`file_exists`・`parent_missing` ではなく `internal`（`to_check_failed`）で断り、`detail` に OS の
      エラーの番号を整数で載せます（`{"reason":"to_check_failed","check":<段>,"os_error":<値>}`。`check` は失敗した確かめで、
      `"to"`（`to` そのもの）か `"parent"`（親のフォルダ）。`os_error` は mac では `errno`、Windows では `GetLastError()` の値で、
      鍵は OS によらず同じ。どれも必ず付ける）。`to` はリンクをたどらずに聞く（mac の `lstat`。Windows の `GetFileAttributesW` もリパースポイントを
      たどらない）ので、壊れたシンボリックリンクがある `to` も `file_exists` になります（OS で揃う）。呼ぶ側がファイルやフォルダが消えたと読み違え
      ないためです。ネットワークの場所そのものが外れているとき（mac の `/Volumes` の下のマウントが外れた、割り当てた
      ドライブ文字が無い）は、OS がパスの無いときと同じ番号（`ENOENT`・3）を返すので見分けられず、「無い」になります。
      逆に、UNC のサーバーの名前が存在しない（綴りの誤り・名前の解決の失敗）ときも、Windows は届かないときと同じ 53 を
      返すので、`parent_missing` ではなく `to_check_failed` になります（名前の解決を待ってから返る）。
      **この「無い」の表は OS の振る舞いで、SDK リファレンスに根拠の節はありません。** 段 5 で、mac と Windows の `VwCliTests`
      が本物の呼び出しで確かめます（ファイルが無い・親のフォルダが無い・途中の段がファイル。途中の段がファイルのとき
      Windows が 3 と 267 のどちらを返すかもここで決まる）。共有が無い（67）は CI の環境に共有のサービスがあるとは限らない
      ので、差し替えの聞く関数で手順を確かめ、本物の 67 は段 5 の実機の確認で見ます。表に無い番号が「無い」の場面で返れば、表に足します。
    - **保存そのものの途中でつながりが切れたとき**に Vectorworks がエラーのダイアログを出すかは、確かめていません。
      出たときは、ほかのダイアログと同じく、呼ぶ側がダイアログのコマンドで読んで押します（[設計「本番で常駐させる
      前提」](../design.md#本番で常駐させる前提安全)。コマンドは [SDK リファレンス #302](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/302) の結果で決める）。
    **止まることは SDK リファレンスでは調べません**（OS の呼び出しがネットワークを待つことは SDK の挙動ではなく、止まるのは
    明らかなので、利用者の判断で CLAUDE.md の 7 の例外とした。例外は [SDK の調査](../sdk-research.md)にも書く。SDK リファレンスに根拠の節は無い。
    保存の途中で切れたときのダイアログも同じ例外で、出ればダイアログのコマンドで扱うので、出るかどうかで実装は変わらない）。
  - **`to` の親のフォルダが無ければ（ファイルであれば）、`internal` で断ります（`parent_missing`）。** SDK もダイアログを
    出さずに断り（50331649）、フォルダは作りませんが、理由を分かりやすくするために先に確かめます。親のフォルダは、
    `to` の最後の区切りまでで作ります。ルートの直下の `to` では、区切りを含めたルートの形にします（`C:\a.vwx` は `C:\`、
    `/a.vwx` は `/`、`\\server\share\a.vwx` は `\\server\share\`。`C:` はドライブ相対、空の文字列は「無い」になるため）。
  - **`save` の状態の確かめ（`internal`）は、`to_is_open` → `file_exists` → `parent_missing` の順に行います**（OS の呼び出しが「無い」以外の理由で失敗すれば、その段で `to_check_failed`。[冒頭](#最初に提供する道具)の 4）。開いている図面と
    一致する `to` は、そのフォルダが消されていても `to_is_open` を返します。
  - **`to` に既にあるファイルを渡しても保存できません**（SDK が `GSError` 50331649 を返し、何も起きない。
    mac・Windows とも、どの形で渡しても）。**保存先が別の図面で開いているファイルなら、SDK が警告のダイアログを
    出して人が OK を押すまで戻りません**（mac も Windows も、開いている図面のパスと文字列が完全に一致するときだけ出る）。
    そこで、呼ぶ前に次の順に確かめて `internal` で断ります。
    1. `to` が開いている図面のパスと一致する（比べ方は下記「パスの一致の判定」）。保存先が開いている図面だと
       示すため（`to_is_open`）、先に確かめます。
    2. `to` がディスクにある（ファイルシステムに聞く）。大文字と小文字・8.3 の短い名前の違いは
       ファイルシステムが吸収するので、文字列の比べ方に依らずに防げます。

    上書きしたければ、呼ぶ側が先にファイルを消します。**呼ぶ側が機械的に見分けられるよう、失敗の `detail` に
    `reason` を付けます:** `to_is_open`（保存先が開いている図面）・`file_exists`（保存先が既にある）。どちらも
    `internal` です（`invalid_args` と `internal` の分け方は[冒頭](#最初に提供する道具)）。
    確かめてから保存するまでの間にファイルができた場合は、SDK のエラーを `internal` で返します
    （開いている図面でなければダイアログは出ない）。これらの `reason`（`parent_missing`・`to_check_failed` を含む）を保存を呼ぶ前に返すことは、段 5 で無 SDK のテストで確かめます
    （[実装の順序](plan.md)の段 5）。
  - **`SaveActiveDocumentPath` が 0 以外を返したら、`internal` で失敗し、`detail` に
    `{"reason":"save_failed","gs_error":<値>}` を付けます。** `GSError` は SDK のエラーの番号（`Uint32`）で、値ごとの
    意味は SDK に書かれていません（実測で出たのは 50331649＝`0x03000001` だけで、保存先が既にあるときも長すぎるときも
    同じ値）。そのため理由を値で分けず、値をそのまま載せます。
  - **保存そのものは取り消されません。** ただし、**図面に PIO を初めて足したあとの最初の保存**は、取り消しの履歴に
    空の段を 1 つ積みます（Vectorworks の振る舞いで、別名保存も上書き保存も、mac も Windows も同じ。利用者がそのあと取り消しを押すと、1 回目は
    何も戻らず、2 回目から戻る。履歴は消えない）。それ以外の `save` は積みません。Vectorworks の振る舞いで、プラグインからは
    避けられません（[Findings「Undo」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Undo.md)の
    「保存はいつも段を積むわけではない」。#288）。
  - 内部では `GetOpenFilesList` からパスの一致する項目の `fileRef` を引き、`SwitchToOpenFile` →
    `SaveActiveDocumentPath` で保存します。**保存を呼んだあとは、成否によらず（`save_failed` でも）、前面の図面とカレントレイヤを保存の前に戻します**
    （戻すのも `SwitchToOpenFile`。対象がもともと前面の図面なら切り替えないので戻さない。結果の `path` は戻す前に読む。
    上記「結果の `path`」）。`SwitchToOpenFile` で前の図面へ戻すと、その図面のカレントレイヤも元どおりになります
    （[Findings「指定した図面を保存する・閉じる」の手順](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#手順getopenfileslist-で引きswitchtoopenfile-で前へ出し前面を戻す)）。
    `invalid_args` と、保存を呼ぶ前に返す `internal`（`no_document`・`to_is_open`・`file_exists`・
    `parent_missing`・`to_check_failed`）は、前面を切り替える前に断るので、前面は動きません。
- **パスの一致の判定。** `document` は `documents` が返した文字列と**完全に一致**するものを探します（呼ぶ側は
  `documents` の結果をそのまま渡す）。`to` は新しいパスで、開いている図面の保存先との衝突を**見逃さない側**に倒して
  比べます: 絶対パスを要し（相対パスは `invalid_args`）、Unicode を正規化（NFC。macOS のファイル名は
  NFD で返ることがある）し、**大文字と小文字を区別せず**に比べます。大文字と小文字だけが違う別のファイルも断る
  ことになりますが、警告のダイアログが出るより安全です（ダイアログが出るのは文字列が完全に一致するときだけなので、
  緩く比べて断れば必ずその手前で止まる）。ディスクにあるファイルはこの比べ方によらず 2 つ目の確かめで断るので、
  この比べ方は、開いている図面との衝突を `to_is_open` として示すためのものです（8.3 の短い名前と長い名前の違いは
  ここでは見分けず、`file_exists` で断る）。
  - **警告が出るのは、mac も Windows も文字列が完全に一致するときだけです**（ファイルが無くても出る。#288）。それでも
    緩く比べるのは、開いている図面のファイルを移したあとに、大文字と小文字・NFC と NFD だけが違う `to` で同じファイルを
    指す 2 枚目ができるのと、Windows で NFD の別のファイル（NTFS は区別する）に見た目が同じ図面ができるのを防ぐためです。
  - **NFC への正規化と、大文字と小文字を区別しない比較は OS の呼び出しで行います**（mac は CoreFoundation の
    `CFStringNormalize`・`CFStringCompare`、Windows は `NormalizeString`・`CompareStringOrdinal`）。外部のライブラリは
    足しません。
    - **畳み方は OS の呼び出しのとおりで、mac と Windows で一部の文字が違いえます**（`CFStringCompare` の
      `kCFCompareCaseInsensitive` は Unicode の case folding、`CompareStringOrdinal` の大文字と小文字の無視は OS の表での
      単純な大文字への写し。どの文字で違うかは確かめていない）。同じ `to` が一方の OS でだけ `to_is_open` になりえますが、
      この比べ方は開いている図面との衝突を示すためのもので、ディスクにあるファイルは `file_exists` で断り、警告が出るのは
      文字列が完全に一致するときだけなので、違いを認めます。ただし、開いている図面のファイルがディスクから移されて
      いると、一方の OS でだけ一致する文字だけが違う `to` は、もう一方の OS では `to_is_open` にも `file_exists` にも
      当たらずに保存され、見た目の近い図面がもう 1 枚できえます。同じファイルを別の綴りで指す場合も、同じく見逃し
      えます。例えば、UNC から開いた図面とその共有に割り当てたドライブ文字の `to`、8.3 の短い名前と長い名前、`subst` の
      ドライブ、ジャンクション・シンボリックリンクを通る綴り、mac の `/var` と `/private/var` です（どれも、開いている
      図面のファイルがディスクから移されているときだけ。ファイルがあれば `file_exists` で断られる）。
    - **OS で分けてビルドするのは、比べ方の薄い包みと、ファイルシステムに聞く関数の包み（mac は `to` に `lstat`・親のフォルダに `stat`、
      Windows は `GetFileAttributesW` を呼び、OS のエラーの番号を「無い」とそれ以外に分ける表を持つ）の 2 つです。** `to` の形の規則は、両方の OS の分をどの OS でもビルドし、
      `_WIN32` で選ぶのはプラグインが使う規則の 1 か所だけです（上の「`to` の形」）。
      保存先の確かめの手順（`to_is_open` → `file_exists` → `parent_missing` の順）は比べる関数と、ファイルシステムに
      聞く関数（「ファイルがある」「フォルダがある」「無い」「確かめられない（OS のエラーの番号）」を返す。親のフォルダがファイルの
      ときを `parent_missing` にするため、ファイルかフォルダかを分ける）を引数で受け取り、
      `src/core` に置いてどの OS でもビルド・テストします。手順のテストでは差し替えの比べ方と、53 などの失敗を返す
      差し替えの聞く関数を渡します（手順を確かめるためのもので、
      出荷する比べ方を確かめるものではない）。**プラグインの `save` が本物の比べ方を渡していることは CI では確かめられない**
      （結び付けは SDK に依る `src/tools` の側）ので、段 5 の実機の確認で、開いている図面と大文字と小文字だけが違う `to` が
      `to_is_open` になることを見ます。
    - **比べ方の包み**（NFC への正規化と大文字小文字を区別しない比較）は、`src/core/PathCompare.h` に宣言し、
      `PathCompareMac.cpp`・`PathCompareWindows.cpp` に分けて置き（`FilesPosix.cpp`・`FilesWindows.cpp` と同じ分け方）、
      APPLE と WIN32 のときだけビルドします。mac は `VwCliCore` に
      `-framework CoreFoundation` を、Windows は `Normaliz.lib`（`NormalizeString`）をリンクします。Linux 用の代わりの
      実装は置きません（出荷しない実装をテストしても、出荷するものについて何も分からないため）。
    - **包みのテスト**は、ほかの無 SDK のテストと同じ `VwCliTests` の中に置きます（別の実行ファイルにしないので、macOS と
      Windows のカバレッジにそのまま入る）。テストのファイル（`tests/PathCompareTests.cpp`）は**どの OS でも**
      `VwCliTests` のソースに並べ、中身を印のマクロ `VWCLI_HAS_PATH_COMPARE` で囲みます。印は、包みをビルドするときに CMake が
      `VwCliCore` に `PUBLIC` で定義し（`VwCliTests` にも届く）、OS で分ける条件は包みの側の 1 か所だけにします。Linux では
      空の翻訳単位になり、skip の分岐は持ちません。**テストのファイルは `__APPLE__` か `_WIN32` なのに印が無ければ `#error` で
      止めます**（包みの側の条件を書き誤ると、テストが黙って抜けて CI が緑のままになるのではなく、コンパイルで落ちる。
      テストのファイルを OS で分けて足すと、そちらの条件の誤りはこの歯止めに届かないので、分けない）。
- **`close`** は対象の図面を閉じます。**保存はせず、未保存の変更は確認なしに捨てます。** 種類は `app` です。
  対象の図面を `SwitchToOpenFile` で前面にしてから `CloseDocument()` を呼びます（前面の図面に効く。
  [Findings「`CloseDocument()` は `false` を返しながら閉じている」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#closedocument-は-false-を返しながら閉じている未保存の変更も黙って捨てる)）。
  - **保存の確認は出しません**（利用者の判断）。メニューの「閉じる」（`DoMenuName("Close", 0)`）は確認を出し、人が
    答えるまで戻らないうえ、何も変えていない図面でも出る場面があるので使いません。`close` を呼ぶことを、呼ぶ側が
    変更を捨てると指定したものとみなします（CLAUDE.md の 5）。**利用者が開いている図面を名指すと、その図面の未保存の
    変更も黙って消えます。** 保存済みの図面に未保存の変更があるかは SDK から前もって読めないので、プラグインは
    確かめません。呼ぶ側は `documents` の `path` をそのまま渡し、残したい変更は先に利用者に保存してもらいます。
  - **mac でも Windows でも、どの図面でも確認は出ません**（無題・保存済み・`OpenDocumentPath` で開いただけ・古い版や
    読み取り専用・使用中のファイルを開いたコピー・大きな図面。[Findings「Windows と、「閉じる」で確認が出る図面」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#windows-と閉じるで確認が出る図面305mac-も)。#305）。
  - **戻り値（成功しても `false`）では成否が分かりません。** 呼び出しの後に `GetOpenFilesList` を読み、対象の図面が
    残っていれば `internal` で失敗し、`detail` に `{"reason":"not_closed"}` を付けます。
  - **`close` も `save` も、呼び出しの中で終わってから戻ります**（大きな図面でも。戻った直後の `GetOpenFilesList` は
    最終の状態で、保存したファイルも書き終わっている。[Findings「大きな図面でも、呼び出しの中で終わる」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#大きな図面でも呼び出しの中で終わる290macwindows-は下の節実務の図面は測っていない)・
    [SDK リファレンス #290](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/290)）。そのため、戻った直後の一覧で成否を決めます。
    - **最中は何も応えません**（`busy` も返せない）。ダイアログが出ない限りタイマーは入り直さないので、mac でも
      Windows でも受け付けの入れ子は起きず、要求はスプールに残ります。
    - **描画したあとの大きな図面は、閉じるのに秒の単位でかかります**（描画の前は 1 秒に満たない。所要は [Findings「Windows と、「閉じる」で確認が出る図面」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#windows-と閉じるで確認が出る図面305mac-も)）。
      実務の図面（参照・レンダリングしたビューポートなど）は測っていないので、長くかかる前提で使います。
    - **呼ぶ側の待つ上限を過ぎても、プラグインは処理を続けて図面を閉じ・保存します。** 呼ぶ側は待ちきれずに終わる
      （失敗とは限らない）ので、`documents` で確かめます。
    - **Windows では呼び出しが数秒を超えると、Vectorworks が「応答なし」と表示されます。** 図面は壊れていないので、失敗と
      読みません。
  - 閉じたあとに前面になるのは、最後に開いた図面です（mac も Windows も）。`close` の前の前面の図面が残っていれば、
    その `fileRef` へ `SwitchToOpenFile` で戻します（カレントレイヤも戻る。前面を閉じたときは戻せない。[Findings「Windows と、「閉じる」で確認が出る図面」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#windows-と閉じるで確認が出る図面305mac-も)）。
- 図面を開く道具は[設計「決めたこと」](../design.md#決めたこと)の「対象の図面」にあります（未実装）。
  `OpenDocumentPath` で開いた図面は、残すなら `save` で新しいパスへ保存してから `close` します（`close` は変更を捨てる）。開いたあとは Vectorworks の最初の描画が刻みを
  止め、次の要求が長く待たされます（上限は無い。[#290](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/290)）。開く道具を足すときは、
  呼ぶ側の待つ上限を過ぎることを前提にします。

## 他のプラグインの機能

手段は `CallPluginLibrary` です（[設計「他のプラグインの道具」](../design.md#他のプラグインの道具)）。
道具の形は[未決 3](open-questions.md#3-ほかのプラグインの機能の呼び出しの形)で決めます。
