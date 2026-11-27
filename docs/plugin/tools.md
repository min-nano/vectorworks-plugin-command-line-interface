# 道具

プラグインが提供する道具の仕様です。版と状態を返す `ping`、図面を**読むだけ**の道具（`layers`・`classes`・
`layer_objects`・`object_counts`）、図面を保存して閉じる道具（`documents`・`save`・`close`）、Vectorworks を終わらせる
`quit` があります。図面に書く道具と、ほかのプラグインの機能を呼ぶ道具は、まだありません
（[設計「これから」](../design.md#これから)）。更新は道具ではなく CLI の `vw2026 install` です
（[インストール](install-and-update.md#インストールと更新vw2026-install)）。

**SDK 呼び出しは、SDK リファレンスで挙動が確かめられているものだけを使います**（無いものはリファレンスで
調べてから足す。[SDK の調査](../sdk-research.md)）。

## 一覧

| 道具 | 種類 | 引数 | 結果 |
| --- | --- | --- | --- |
| `tools` | （予約） | なし | [通信規則「予約された道具」](../protocol.md#予約された道具) |
| `ping` | read | なし | 下記 |
| `layers` | read | `include_sheets`（既定 true） | `{"layers":[{"name","kind":"design"\|"sheet","referenced","scale","objects","current"}],"count"}`（[読む道具](#読む道具layersclasseslayer_objectsobject_counts)） |
| `classes` | read | なし | `{"classes":[名前…],"count"}`（同上） |
| `layer_objects` | read | `layer`（必須）・`limit`（既定 50）・`offset`（既定 0）・`type`（種別の番号。省略＝すべての種別） | `{"layer","unit":"mm","objects":[{"index","type","type_name","pio_name","name","class","bounds":{"left","right","top","bottom"}}],"returned","total"}`（同上） |
| `object_counts` | read | `layer`（省略＝図面全体） | `{"layer"?,"types":[{"type","type_name","count"}],"total"}`（種別番号の昇順。同上） |
| `documents` | read | なし | [図面を扱う道具](#図面を扱う道具documentssaveclose) |
| `save` | app | `document`（必須）・`to`（必須） | 同上 |
| `close` | app | `document`（必須） | 同上 |
| `quit` | app | なし | `{"quitting":true}`。図面が開いていれば `internal` で失敗し、`detail` に一覧（下記） |

## 共通の決まり

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
- `core::Json::asInt64` は綴り（小数部と指数部が無いか）で整数かを決めるので、整数の引数は値で決める読み方の
  `core::Json::integerValue` で読みます（道具の引数の読み方は `src/core/Args.h` の 1 か所）。
- 引数の値の `null` は省略と見なさず、型の違いとして断ります（省くときはキーごと省く）。

**結果の実数（`scale`・`bounds` の値）の桁は、元の値に戻る最も短いものにし、書き方は Go の `encoding/json`
に合わせます**（プラグインの綴りを決まった基準に揃え、その基準を見本で C++ と Go の両側から確かめるため）。
- 桁は指数の形の `std::to_chars`（`std::chars_format::scientific`。精度は指定しない）から取り、書き方は下の決まりで
  組み立てます。形を指定しない `to_chars` をそのまま使わないのは、固定小数点より指数のほうが短ければ指数を選ぶ
  （`100000` が `1e+05`、`0.0001` が `1e-04` になる）のと、大きな整数の値を最も短い桁ではなく正確な値で書く実装が
  ある（libstdc++ は `999999999999999900000` を `999999999999999868928` と書く。固定小数点の `to_chars` も同じ）ためです。
  指数の形の `to_chars` の出力は、規格（[charconv.to.chars]）で小数点の前が 1 桁（値が 0 でなければ 0 ではない。
  `printf` の `%e` の形）、仮数の末尾に 0 が付かない（文字数が最も短い）と決まっているので、仮数の数字と指数を
  そのまま取り出します（先頭と末尾の 0 を除く処理を持ちません）。
  `core::Json` は数を綴りで持つので、実数は書き出す口の `core::Json::real` で作ります。
- **0 と、`1e-6 ≤ |x| < 1e21` は固定小数点で書きます**（`0`・`100000`・`1500000`・`0.0001`・`0.000001`・`2366.025`）。整数の値は
  小数点を付けません（`50.0` は `50`）。上と同じく、呼ぶ側は書き方で整数と実数を区別しません。
- それ以外は指数で書き、指数は符号を付けて先頭の 0 を付けません（`1e+21`・`1e-7`・`1.5e-7`）。
- `-0` も Go と同じく `-0` と書きます（読み手には 0 と同じ値。Go と綴りを揃えるため、符号を落とさない）。
- **実数と綴りの組は見本のファイル（`tests/fixtures/reals.json`）に置き**、C++ のテスト（`tests/NumberTests.cpp`）は
  書き出しの口と、Go のテスト（`cli/internal/toolfixtures`）は `json.Marshal` と照らします。Go と同じ綴りであることを、片側のテストに書いた期待だけで決めないためです。これは道具の結果の
  決まりで通信規則ではないので、見本は通信規則の見本（`protocol/fixtures`。[通信規則の見本](../fixtures.md)）とは
  分けて `tests/fixtures/` に置きます。
- **有限でない値（無限大・NaN）は JSON に書けないので、そのフィールドを `null` にします**（`bounds` は 4 つの
  どれかが有限でなければ `bounds` ごと `null`）。この決まりは道具ごとに書かず、`core` の 1 か所に置きます:
  `core::Json` の実数の口は「有限でなければ `null` を返す」1 つだけにし、道具の結果の実数はどれもそれで作ります。`bounds` は
  `GetObjectBounds` の戻り値（真偽）と外形の 4 つの値を受けて `bounds`（または `null`）を返す 1 つの関数で作ります
  （偽の場合も無 SDK のテストで確かめられるように）。

**引数の確かめは次の順に行い、最初に当たったもので返します**（どの `reason` を返すかを決めておくため）。
1. 引数の形（知らない引数・型・値の範囲・必須の欠け・`save` の `to` の形。[保存先の確かめ](save-target.md#to-の形)）→ `invalid_args`。
2. 図面が無い → `internal`（`no_document`）。**「図面が無い」は、道具が使うものの有無で決めます。** 前面の図面を読む
   道具（`layers`・`classes`・`layer_objects`・`object_counts`）は `GetCurrentLayer()` が `nullptr` か（`ping` の
   `document_open` と同じ基準。`layers` は `current` のためにどのみち呼ぶ）、図面を名指す道具（`save`・`close`）は
   `GetOpenFilesList` の件数が 0 か（名指す図面を引く一覧と同じもの）で決めます。2 つが一致することを確かめたのは、
   起動の直後と全部閉じた後だけです（[Findings「Timers and Notifications」の 4 の 8](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Timers%20and%20Notifications.md)）。件数が 1 以上でも前面の
   図面が無い状態があれば、読む道具は `no_document` で断り、`save`・`close` は名指した図面に働きます（どちらも道具が
   使うものに合っている）。`documents` は失敗にせず空の一覧を返し、`ping` は失敗せずに `document_open` を偽にします。
3. 引数が名指すもの（`document`・`layer`）が実在しない → `invalid_args`。名指すものが 1 つに決まらない
   （`document` に一致する図面が 2 枚以上 → `internal`（`document_ambiguous`）、`layer` に一致するレイヤが 2 枚以上 →
   `internal`（`layer_ambiguous`））。
4. 道具ごとの状態（`save` は[保存先の確かめ「状態の確かめ」](save-target.md#状態の確かめ)の順）→ `internal`。

**道具が返す失敗の `invalid_args` と `internal` は、次のように分けます**（[通信規則「応答」](../protocol.md#応答idresjson)）。

- **`invalid_args`** は、引数そのものが誤っているとき: 知らない引数・型や値の誤り・必須の欠け、引数が名指すもの
  （`document` の図面・`layer` のレイヤ）が実在しない、
  `save` の `to` の形（拡張子・使えない文字など）。
- **`internal`** は、引数は正しく、名指すものも実在するが、それ以外の状態のために実行できないとき: 図面が無い
  （`no_document`）・名指すものが 1 つに決まらない（`save`・`close` の `document_ambiguous`、`layer_objects`・
  `object_counts` の `layer_ambiguous`）・
  図面が開いている（`quit` の `documents_open`）・保存先が埋まっている（`save` の `to_is_open`・
  `file_exists`。`to` は新しく作るファイルを名指すので、既にあることは状態に当たる）・保存先の親のフォルダが無い
  （`parent_missing`）・保存先を確かめられない（`to_check_failed`）・図面が閉じなかった（`close` の
  `not_closed`）・SDK の失敗（`save` の `save_failed`）・道具の中の例外。

呼ぶ側が理由で分岐するときは、`code` ではなく `detail` の `reason` を見ます（`reason` を返す失敗だけ）。

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

図面を読むだけの道具です。使う SDK 呼び出しと挙動は SDK リファレンスで
確かめてあります（[SDK の調査 4](../sdk-research.md#4-道具が使う-sdk-呼び出し)）。どれも前面の図面
（アクティブな図面）を読みます。

**共通の決めごと:**

- **図面が開かれていなければ（`GetCurrentLayer()` が `nullptr` か。[共通の決まり](#共通の決まり)の確かめの順の 2）`internal` で失敗し、`detail` に `{"reason":"no_document"}` を付けます。**
  SDK は図面が無くても落ちずに空の列を返します（[Findings「Layers and Stories」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Layers%20and%20Stories.md)の「OS タイマーの刻みの中・図面 0 枚」・
  [Findings「Attributes and Classes」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Attributes%20and%20Classes.md)の「タイマーの刻みから呼べる。図面 0 枚でも落ちない」）が、
  空の結果（`object_counts` の `total:0` など）を「中身の無い図面」と見分けられなくなるので、失敗にします。
  `documents` は失敗にせず、空の一覧を返します。`ping` は `current_layer` を `null` にします（[`ping`](#ping)）。
- **`layer` はレイヤの名前で、`layers` の `name` と完全に一致するものを探します**（リソースの参照で入ってきた
  レイヤは `NNA#<n>_<元の名前>` の形のまま渡す）。一致しなければ `invalid_args` で断ります。一致するレイヤが 2 枚以上
  あれば、どれも読まずに `internal` で失敗し、`detail` に `{"reason":"layer_ambiguous"}` を付けます（Vectorworks は
  同じ名前のレイヤを参照で入れることも断る（[Findings「Layers and Stories」の「参照で入ってきたレイヤ」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Layers%20and%20Stories.md#参照で入ってきたレイヤ参照の方法で-2-通りの姿になる見分けは-isobjectfromreferencedfilelayer)）ので
  ふつうは起きないが、確かめていない経路で同じ名前が 2 枚になっても、黙ってどちらかを読まない。`document_ambiguous` と同じ扱い）。
  `layer` を渡したときは、名前でそのレイヤを探し、その直下だけを辿ります（頁ごとに図面全体を辿らない）。図面が無いときは、`layer` を
  確かめる前に `no_document` で失敗します。
- **並び順は SDK の列の順で、背面→前面です**（`layers` も `layer_objects` も同じ向き）。画面の並びに
  合わせて並べ替えることはしません（呼ぶ側が並べ替える）。

### `layers`

`GetDrawingHeader()` → `FirstMemberObj` → `NextObject` の列を、型が `kLayerNode`（31）のものだけに絞って返します
（列の末尾に、レイヤではない型 0 のものが 1 つ居る。[Findings「Layers and Stories」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Layers%20and%20Stories.md)の
「レイヤを列挙する」）。

| フィールド | 値 |
| --- | --- |
| `name` | レイヤの名前（`GetObjectName`）。リソースの参照で入ってきたレイヤは `NNA#<n>_<元の名前>`、レイヤの参照で入ってきたレイヤは元の名前のまま（下記） |
| `kind` | `design`（`ovLayerType` が 1 か 3）・`sheet`（2）。それ以外の値（Findings で確かめていない種類）は `null` |
| `referenced` | 参照で入ってきたレイヤか（`IsObjectFromReferencedFile(layer)`。参照の方法によらず真。下記） |
| `scale` | 縮尺の分母（`GetLayerScaleN`。1/50 なら `50`、1:1 なら `1`）。SDK は `double_gs` で返すので実数として書きます（[Findings「レイヤの縮尺」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Layers%20and%20Stories.md#レイヤの縮尺setlayerscalen-は黙って効かないことがある)。確かめた縮尺はどれも整数で、整数でない縮尺は確かめていない）。**シートレイヤは SDK の返すとおり常に `1`**。有限でなければ `null`（[共通の決まり](#共通の決まり)） |
| `objects` | レイヤの直下の図形の数（下記「数え方」） |
| `current` | 現在のレイヤか（`GetCurrentLayer()` とハンドルが一致するか） |

- **並びは作った順で、デザインレイヤとシートレイヤが入り混じります。** オーガナイザはデザインレイヤだけを
  この逆の順（`#` の 1 が最前面）に見せ、シートレイヤはこの順のまま見せます
  （[Findings「Layers and Stories」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Layers%20and%20Stories.md)の「順序」）。
- `include_sheets` が偽なら、`kind` が `sheet` のものを除きます（`kind` が `null` のものは除かない）。
- **`kind` が `null` のレイヤも除かずに返し**、`objects` も数えます。`object_counts` の図面全体の和と食い違わない
  ようにするためです。
- **参照で入ってきたレイヤも返します**（`referenced` が真）。別の図面を参照すると、参照元のデザインレイヤが同じ列に
  型 31 のまま入り、**参照の方法で 2 通りの姿になります**（mac と Windows で同じ。[Findings「Layers and Stories」の
  「参照で入ってきたレイヤ」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Layers%20and%20Stories.md#参照で入ってきたレイヤ参照の方法で-2-通りの姿になる見分けは-isobjectfromreferencedfilelayer)。
  [SDK リファレンス #310](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/310)）。

  | | リソースの参照（`CreateReference`・オーガナイザの既定） | レイヤの参照（オーガナイザで「レイヤを参照」にして、参照レイヤを選ぶ） |
  | --- | --- | --- |
  | `name` | `NNA#<n>_<元の名前>` | 元の名前のまま |
  | `kind` | `design`（`ovLayerType` が 3） | `design`（`ovLayerType` が 1） |
  | `referenced` | 真 | 真 |
  | `objects` | 0（直下に図形が見えない） | 参照元の図形の数（直下に見える） |
  | 列の位置 | 自分のレイヤの後 | 列の先頭（自分のレイヤより前） |
  | `scale` | 参照元の縮尺 | 参照元の縮尺 |

  - **参照由来かは `IsObjectFromReferencedFile(layer)` で決めます。** `ovLayerType` の 3 はリソースの参照でしか立たず、
    レイヤの参照で入ったレイヤは 1 で、自分のレイヤと見分けられないためです。
  - **レイヤの参照で入った図形も数えます**（`objects`・`layer_objects`・`object_counts`）。情報パレットも「すべてを選択」で
    これを「N 図形 - ロック状態」と数え（下記「数え方」と同じ数）、図形は参照元の型・クラス・外形のまま見えるためです。
    参照の中身を除きたい呼ぶ側は、`referenced` が真のレイヤを除きます（参照で入ったレイヤの直下の図形は、どれも参照元の
    ものです。図形ごとの印は返しません）。`object_counts` の図面全体も、これらを含んだ和です。
  - 参照を解除するとき「参照レイヤ: ファイルに残す」を選ぶと、レイヤと図形は自分のものになり、`referenced` は偽に
    変わります。

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
  `invalid_args` で断ります（数の受け方は[共通の決まり](#共通の決まり)）。`offset` が `total` 以上なら `objects` は空で
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
  返した図形と、4 つの値のどれかが有限でない図形は `null` です（[共通の決まり](#共通の決まり)）。偽を返すと確かめたのは
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

- **Vectorworks が起動を終えるまでは、終了を頼まずに `internal` で失敗します**（`detail` は
  `{"reason":"starting","notification_registered":<真偽>}`。図面の一覧を見るより先に返す）。`error` は「Vectorworks が起動を
  終えていないので終了しませんでした」を含む固定の文言です。
  - **`notification_registered`** は、起動を終えた目印の通知（下記）を登録できたかです。偽なら印はいつまでも立たないので、
    起動を待って呼び直しても通りません（`error` にも「起動を終えたことを知る通知を登録できていません」が付く）。プラグインは
    登録できるまで刻みごとにやり直すので、偽が続くのは登録が通らない場合だけです。呼ぶ側は、真なら起動を待って呼び直し、
    偽が続けば呼び直しをやめて人に知らせます。
  - 起動を終える前に終了を頼むと、mac では、Cinema4D（Cineware）の起動を待つ間なら起動画面のまま止まって強制終了でしか
    止まらず、「開発元不明のプラグイン」のダイアログの間なら頼みが黙って捨てられます。どちらも呼んだ側からは失敗が見えません
    （[SDK リファレンス #312](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/312)。[Findings「Timers and Notifications」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Timers%20and%20Notifications.md) の 4 の 9）。
  - プラグインは最初の刻み（起動画面の頃）から応えるので、`launch` のあと `status` が動いていると答え、`ping` が応えても、
    まだ起動を終えていないことがあります。
  - **起動を終えたかは、最初の `kNotifyBeforePendingUpdate` で知ります**（Vectorworks のアイドルから届く通知。起動画面・
    Cineware の起動待ち・「開発元不明のプラグイン」のダイアログの間には届かず、ホーム画面が出たあとに初めて届く。その後の
    刻みから終了を頼むとふつうに終わる。[SDK リファレンス #317](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/317)。同 4 の 9「起動を終えた目印」）。ダイアログの間は、
    利用者が「続ける」を押すまで `starting` で断られ続けます。呼び直すか・いつまで待つかは呼ぶ側が決めます
    （[設計「CLI はプリミティブに保つ」](../design.md#cli-はプリミティブに保つ)）。
  - Windows の起動の途中の振る舞いは測っていません（[#316](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/316)。仮想環境で DirectX を有効にできない）。
    mac と同じ作りにしてあり、起動を終えたあとに通知が届いて `quit` が受け付けられることは Windows の実機で確かめました
    （起動の途中に `starting` で断られることは Windows では確かめられていない。[実機で確かめていないこと](#実機で確かめていないこと)）。
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
  | `reason` | `documents_open`（`quit` の失敗の理由を表す固定の綴り。呼ぶ側はこれで分岐する。起動を終えていないときは上の `starting`、閉じた図面が中に残って片付けられなかったときは下の `closed_documents_remain` で、どちらも `documents` を持たない） |
  | `documents[].name` | 図面の名前 |
  | `documents[].path` | 図面のパス（`documents` と同じ。`untitled` が真の図面は「アプリケーションのフォルダ＋名前」（「名称未設定 N」・「<元の名前> コピー 1」など）でファイルとしては存在しない） |
  | `documents[].untitled` | ディスクのファイルに結び付いていない図面か（`documents` の `untitled` と同じ） |
- **一覧が空でも、`close` で閉じた図面が Vectorworks の中に残っていれば、終了を頼む前に片付けます**
  （[閉じた図面の片付け](#閉じた図面の片付け)）。上限の回数までに片付かなければ、終了を頼まずに `internal` で失敗し、
  `detail` に `{"reason":"closed_documents_remain"}` を付けます。片付けのあとにもう一度一覧を見て、図面が開いていれば
  （片付けの間に開いたもの）、終了を頼まずに `documents_open` で断ります。
- **図面が 0 枚（残った図面も無い）なら、保存の確認は出ません。** 応答を書いてから、受け付けの外で
  `gSDK->CloseAllFilesAndQuitVectorworks(true, false)` を頼みます（[構成「受け付けの流れ」](architecture.md#受け付けの流れ)）。
  第 1 引数は常に真です（`quit` は図面を保存するか捨てるかを決めない。決めるのは呼ぶ側で、`save`（新しいパスへ）・`close`（変更を捨てる）で先に片付ける）。確かめてから頼むまでの間に利用者が
  図面を開くと、保存の確認が出うる競合が残りますが、`quit` は占有の中でだけ呼べ、呼ぶ側が図面を閉じた直後に
  呼ぶ使い方なので、通常は起きません。
- **図面を保存して閉じるのは呼ぶ側です。** 保存するか捨てるかも呼ぶ側が決めます（`quit` に保存する・捨てる指定は持たず、
  CLI にも足さない。[設計](../design.md#cli-はプリミティブに保つ)）。残したい図面（変更）は `save` で新しいパスへ保存してから、
  どの図面も[下記の `close`](#図面を扱う道具documentssaveclose)で閉じます。`close` は保存の確認を出さず、未保存の変更を捨てます。
- **`close` で名指せない図面が開いていると、CLI だけでは終了できません。** パスを読めなかった図面（`path` が空）と、
  `path` がほかの図面と重なる図面（`document_ambiguous`。別々のフォルダにある同じ名前の古い版のファイルを開いたものなど）は
  `close` で閉じられないので、開いている間 `quit` は `documents_open` で断られ続けます。その図面は利用者が Vectorworks で閉じます
  （呼ぶ側が人に頼む）。CLI から扱えるようにするのは、名指し方（`fileRef` など）を SDK リファレンスで確かめてからにします
  （[下記「`document`」](#図面を扱う道具documentssaveclose)）。
- **占有の中でだけ呼べます**（[通信規則「占有」](../protocol.md#占有セッション)。占有していなければ
  `no_session`、ほかが占有していれば `occupied` で断られる）。Vectorworks が終われば占有も消えます。
- **終了の依頼の最中は断られます**（`busy`。[通信規則「Vectorworks が処理中」](../protocol.md#vectorworks-が処理中)）。
  上の競合で保存の確認が開いている間に届いた 2 つ目の `quit` も `busy` で断られるので、終了の依頼は重なりません。
  その間は `ping` と読む道具も `busy` で断られ、`tools`・`session start`・`session end` だけが応えます（表に無い道具は `unknown_tool`）
  （[構成「Vectorworks が処理中」](architecture.md#vectorworks-が処理中)）。
- **再起動の手段は持ちません**（第 2 引数は常に偽）。起動し直すのは呼ぶ側で、`session start` で占有を
  取ってから `quit`（`--session` を付ける）・止まるのを待つ・`launch`・動き出すのを待つ、を組み合わせます（更新を
  挟む形を含めて[インストール「流れ」](install-and-update.md#流れ)）。
- 終了を見届けるのは呼ぶ側の役割です（`vw2026 status` を止まっていると答えるまで繰り返す。保存の確認を開いている間の扱いは
  [通信規則「生存の判定」](../protocol.md#生存の判定)）。

## 図面を扱う道具（`documents`・`save`・`close`）

`quit` の前に図面を片付ける（残したい図面を新しいパスへ保存する・図面を閉じる）ための道具です。使う SDK 呼び出しは SDK
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
| `close` | app | `document`（必須） | `{"closed":true}`（閉じた図面が中に残って片付けられなければ `"closed_documents_remain":true` も） |

```json
{"documents":[{"name":"plan.vwx","path":"/Users/me/plan.vwx","untitled":false,"active":true},{"name":"名称未設定 1","path":"/Applications/Vectorworks 2026/名称未設定 1","untitled":true,"active":false}],"count":2}
{"document":"/Applications/Vectorworks 2026/名称未設定 1","path":"/Users/me/tmp/a.vwx"}
{"closed":true}
```

上から `documents`・無題の図面を `to` へ保存した `save`・`close` の結果です。

- **図面が 1 枚も開いていなければ、`save`・`close` は `internal` で失敗し、`detail` に `{"reason":"no_document"}` を
  付けます**（`GetOpenFilesList` の件数で決める。[共通の決まり](#共通の決まり)の 2。`document` を確かめる前に返す）。
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
  Windows の古い版のファイルは、実機でも見られていません（[実機で確かめていないこと](#実機で確かめていないこと)）。
  **`untitled` が真の図面の `path` は「アプリケーションのフォルダ＋名前」**で、ファイルとしては存在しません（名前は
  「名称未設定 N」・「<元の名前> コピー 1」・「<元の名前> v2026.vwx」など。N はセッション全体の連番で、開いている間は
  重ならない）。**`untitled` が真の図面は、`path` がほかの図面と重なることがあります**（下記。その図面は `save`・
  `close` で名指せない）。呼ぶ側は `path` を鍵にして一覧を扱うと項目を取りこぼします。
- **`document`** は対象の図面のパスで、`documents` の `path` を渡します。ディスクのファイルに結び付いた図面どうしは、
  パスで一意に決まります（同じファイル名を別のフォルダに保存した 2 枚も見分けられる）。`documents` が返した文字列と
  **完全に一致**する図面を探し（呼ぶ側は `documents` の結果をそのまま渡す）、一致しなければ `invalid_args` で断ります。パスを読めなかった図面（`documents` の `path` が空）は名指せず、空の `document` は
  `invalid_args` で断ります（空の文字列でその図面を閉じない）。
  - **`untitled` が真の図面のパスは、一意とは限りません**（Findings も、無題の図面を指すなら
    `fileRef` のほうが確かだとしている。[Findings「指定した図面を保存する・閉じる」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#指定した図面を保存する閉じる前面に依存せず)）。
    Vectorworks がアプリケーションのフォルダと名前から作るので、別々のフォルダにある同じ名前の古い版のファイルを開いた
    2 枚は、同じ `<アプリ>/<名前> v2026.vwx` になりえます（「名称未設定 N」の N は重ならない）。SDK リファレンスでは
    確かめておらず、このプラグインの mac の実機の確認で、古い版の 2 枚は重なり、読み取り専用のファイルを開いたコピー
    （「コピー N」）は重ならないことを見ただけです（[SDK の調査「残っているもの」](../sdk-research.md#残っているもの)）。利用者がアプリケーションの
    フォルダに同じ名前のファイルを保存して開いていれば、`untitled` が偽の図面とも重なりえます。**`untitled` によらず、
    一致する図面が 2 枚以上あれば、どれも保存・閉じずに `internal` で失敗し、`detail` に `{"reason":"document_ambiguous"}`
    を付けます**（名指すものは実在するので `invalid_args` にしない。[共通の決まり](#共通の決まり)の 3 で、名指すものの
    実在の直後に確かめる）。黙ってどれか 1 枚に効かせると、呼ぶ側の思っていない図面を閉じうるためです。
    呼ぶ側は、利用者に一方を閉じてもらうなどして見分けられるようにします。
  - **`path` は正規化しません。** SDK が返した文字列をそのまま返します。Windows では、保存したときに渡された
    文字列がそのまま残り、区切りだけが `\` に揃います（ドライブ文字の大小・名前の大小・8.3 の短い名前は直らない）。
    同じファイルでも、保存したときの渡し方しだいで別の文字列になります。
- **`save`** は、名指した図面を `to` の新しいパスへ保存します。種類は `app` です（図面の中身は変えず、ファイルに書く。
  [ブリッジ「道具の表」](bridge.md#道具の表)）。`to` は必須で（省くと `invalid_args`）、既にあるファイルは断ります（下記「`to` の確かめ」）。
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
  - **`to` は、保存を呼ぶ前に形と状態を確かめます**（規則と理由は[保存先の確かめ](save-target.md)が真実）。
    Vectorworks は形を確かめずにその名前で保存するものがあり、開いている図面のパスへの保存は警告のダイアログを出して
    人が押すまで戻らないためです。
    - **形**（`invalid_args`）: 絶対パス（mac は `/` から始まるもの、Windows は `X:\` で始まるものと UNC。Windows は
      `/` を含むものを断る）で、拡張子が `.vwx` であること。`.`・`..`・空の段・制御文字と、OS ごとの使えない文字・
      長さの上限を超えるものは断ります。プラグインは正規化して渡さず、断ります。
    - **状態**（`internal`）: 次の順に確かめ、最初に当たったものの `reason` を `detail` に付けます。
      1. `to_is_open`: `to` が開いている図面のパスと一致する（NFC に正規化し、大文字と小文字を区別せずに比べる）。
      2. `file_exists`: `to` が既にディスクにある（上書きしたければ、呼ぶ側が先にファイルを消す）。
      3. `parent_missing`: `to` の親のフォルダが無い（フォルダでない）。
      - 2・3 で OS の呼び出しが「無い」以外の理由で失敗したら `to_check_failed`
        （`{"reason":"to_check_failed","check":"to"|"parent","os_error":<OS のエラーの番号>}`）。
    - **ネットワークの場所の保存先では、サーバーが届かないと、OS が失敗を返すまで Vectorworks が止まります。**
      そうした保存先には長い `--timeout` を付けます（[保存先の確かめ「ネットワークの場所」](save-target.md#ネットワークの場所)）。
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
    `SaveActiveDocumentPath` で保存します。**`SwitchToOpenFile` の戻り値だけを信じず、切り替えたあとの一覧で前面が名指した
    図面になったことを確かめてから保存します**（前面が変わらないまま保存を呼ぶと、利用者が前面に開いている別の図面を
    `to` へ保存してしまう）。前面にできなければ（`SwitchToOpenFile` が偽を返したときも）、保存を呼ばずに前面を戻し、
    `internal`（`reason` なし）で失敗します（確かめていない場面）。保存が 0 を返したのに保存の直後の一覧に前面の項目が無いときも、
    `internal`（`reason` なし）で失敗します（保存は済んでいるので、呼ぶ側は `documents` で確かめる）。**保存を呼んだあとは、成否によらず（`save_failed` でも）、前面の図面とカレントレイヤを保存の前に戻します**
    （戻すのも `SwitchToOpenFile`。対象がもともと前面の図面なら切り替えないので戻さない。結果の `path` は戻す前に読む。
    上記「結果の `path`」）。`SwitchToOpenFile` で前の図面へ戻すと、その図面のカレントレイヤも元どおりになります
    （[Findings「指定した図面を保存する・閉じる」の手順](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#手順getopenfileslist-で引きswitchtoopenfile-で前へ出し前面を戻す)）。
    `invalid_args` と、保存を呼ぶ前に返す `internal`（`no_document`・`to_is_open`・`file_exists`・
    `parent_missing`・`to_check_failed`）は、前面を切り替える前に断るので、前面は動きません。
    保存を呼ぶ口が例外を投げたときも、前面を戻してから `internal` で応えます。
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
    残っていれば（`document` と完全に一致するパスが残っていれば）`internal` で失敗し、`detail` に `{"reason":"not_closed"}` を付けます。
    閉じる前も `save` と同じく、切り替えたあとの一覧で前面が名指した図面になったことを確かめます（前面が変わらないまま
    `CloseDocument()` を呼ぶと、利用者の別の図面を変更を捨てて閉じてしまう）。前面にできなければ、閉じずに前面を戻し、
    `internal`（`reason` なし）で失敗します。
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
  - **最後の図面を閉じたら（閉じたあとの一覧が空なら）、中に残った図面を片付けます**
    （[閉じた図面の片付け](#閉じた図面の片付け)）。片付かなくても、名指した図面は閉じたので失敗にせず、結果に
    `"closed_documents_remain":true` を足して知らせます（`{"closed":true,"closed_documents_remain":true}`。片付いたとき・
    片付けが要らなかったときは載せない）。呼ぶ側は、利用者がメニューで終了する前に `quit` で終了するか、人に知らせます
    （`quit` は終了を頼む前にもう一度片付け、片付かなければ断る）。
  - 閉じたあとに前面になるのは、最後に開いた図面です（mac も Windows も）。`close` の前の前面の図面が残っていれば、
    その `fileRef` へ `SwitchToOpenFile` で戻します（カレントレイヤも戻る。前面を閉じたときは戻せない。[Findings「Windows と、「閉じる」で確認が出る図面」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#windows-と閉じるで確認が出る図面305mac-も)）。
- **図面を開く道具はまだありません**（[設計「これから」](../design.md#これから)）。
  `OpenDocumentPath` で開いた図面は、残すなら `save` で新しいパスへ保存してから `close` します（`close` は変更を捨てる）。開いたあとは Vectorworks の最初の描画が刻みを
  止め、次の要求が長く待たされます（上限は無い。[#290](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/290)）。開く道具を足すときは、
  呼ぶ側の待つ上限を過ぎることを前提にします。

## 閉じた図面の片付け

`CloseDocument()` は、先に開いた図面を後に開いた図面が開いている間に閉じると、閉じた図面を Vectorworks の中に
残します（以下「残った図面」。[Findings「閉じた図面が残り、終了で保存の確認が出る」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#閉じた図面が残り終了で保存の確認が出る321323macwindows)。
[SDK リファレンス #321](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/321)）。残った図面は `GetOpenFilesList` に出ず（`documents` も空）、一覧が空になった
あとの終了（`quit` も、利用者のメニューの「終了」も）で、一番上に残った図面が「変更あり」（無題なら変更が無くても）なら
保存の確認が出て、人が答えるまで止まります。`close` と `quit` は、これを次のように片付けます。

- **見分け方と片付け方。** 一覧が空なのに `GetActiveDocument` が真なら残っています。一覧が空であることを回ごとに
  確かめながら `CloseDocument()` を呼び、`GetActiveDocument` が偽になるまで 1 枚ずつ消します（`core::clearClosedDocuments`。
  確認は出ない。残った図面は `close` が変更を捨てると指定されて閉じたものなので、変更を捨てる）。回数の上限は 64 回です。
- **`close` は、最後の図面を閉じたとき（閉じたあとの一覧が空のとき）に片付けます**（利用者がメニューで終了しても
  確認が出ないように）。片付かなければ、成功のまま結果で知らせます（[`close`](#図面を扱う道具documentssaveclose)）。
- **`quit` は、終了を頼む前に片付けます。** `close` の側の片付けが届かない道への備えです（ほかのプラグインやスクリプトの
  `CloseDocument()` が最後の図面を閉じた・`close` の片付けが上限で止まった、など）。外すと、`quit` が
  `{"quitting":true}` を返したあとに見えない図面の保存の確認が出て、人が答えるまで Vectorworks が止まります。残った
  図面が無ければ `GetActiveDocument` を 1 回呼ぶだけです。片付かなければ断ります（[`quit`](#quit)）。
- **図面が開いている間は片付けません**（`CloseDocument()` は開いている図面を閉じてしまう）。ほかの図面が開いている間に
  `close` した図面は、最後の図面が閉じるまで中に残ります。最後の図面を `close` 以外（利用者の UI・ほかのプラグイン）で
  閉じたときは、プラグインは片付けません（勝手に動かない。CLAUDE.md の 5）。残っていても、`quit` で終了すれば `quit` が
  片付けます。最後の図面を利用者が UI で閉じると、残った図面も消えます（Windows（Vectorworks 2025）。mac は推定。同じ節）。
- **閉じる順の工夫（後に開いたものから閉じる）では避けません。** 残るかを決める図面の並びは SDK から読めないためです。
- **残った図面の状態で `SaveActiveDocumentPath` を呼ぶと Vectorworks が落ちます**（同じ節）。`save` は一覧が空なら
  `no_document` で断り、保存を呼ばないので当たりません。
- **Windows も同じく片付けます**（Vectorworks 2025 の Windows でも図面が残り、この片付けで確認が出なくなる。同じ節。
  Vectorworks 2026 の Windows は測っていない。[#323](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/323)）。

## 実機で確かめていないこと

道具は mac と Windows の実機で確かめてから出しましたが、次のものは実機で見られていません（Windows の確認に使った
仮想環境で Vectorworks がメモリの不足で落ち続けた・必要な状況を起こせなかった。実機の確かめ方は
[実機の確かめ方](verification.md)）。関わるところを変えるときは、見られるようになっていれば先に確かめます。

| 見られていないもの | 分かっていること |
| --- | --- |
| Windows の参照で入ったレイヤ（`layers` の `referenced`・`objects`） | SDK の振る舞いは Windows でも測ってある（[SDK リファレンス #310](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/310)）。mac の実機では 2 通りとも確かめた |
| Windows の古い版のファイルを開いた図面の `untitled` と、同じ名前の 2 枚の `path` の重なり | SDK リファレンスでも測れなかった（[#291](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/291)）。`untitled` が偽でも、`save` は新しいパスへしか保存せず `close` は保存しないので、利用者のファイルは置き換わらない。mac の実機では `untitled` が真で、`path` が重なり、`save`・`close` が `document_ambiguous` で断った |
| Windows の読み取り専用のファイルを開いたコピーの `close` と、同じ名前の 2 枚の `path` | `close` で確認が出ないことは SDK リファレンスで分かっている（[#305](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/305)）。mac の実機では確認なしに閉じ、`path` は重ならなかった |
| Windows の、前もって作った共有（UNC）への `save` | 管理共有（`\\localhost\C$`。確認に使った環境では Vectorworks が昇格して動いていた）への保存は確かめた |
| Windows のシートレイヤなどの、情報パレットの数・外形との比較の一部 | 数は見本の図面を作ったスクリプトの数と一致した。mac の実機では情報パレットと一致した |
| 前面を確かめてから保存・閉じること・`layer_ambiguous`・`layer` を渡したときにそのレイヤだけを辿ること | 実機の確認のあとに入れた。無 SDK のテストで確かめてある |
| `quit` の側の[閉じた図面の片付け](#閉じた図面の片付け) | 要る状況を実機で起こせていない（最後の図面を UI で閉じた形は、片付けの無い版でも確認が出なかった）。無 SDK のテストで確かめてある |
| Windows の起動の途中の `quit`（`starting` で断ること） | ふつうの起動では `session start` が応える前に起動を終えていて当てられない。mac の実機では確かめた。起動を終えた目印の通知が Windows でも届くことは確かめた |
| Windows の Vectorworks 2026 での `quit` と閉じた図面の片付け | Windows の確認は Vectorworks 2025 で行った（[#323](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/323)） |
