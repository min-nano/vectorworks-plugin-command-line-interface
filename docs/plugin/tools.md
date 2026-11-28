# ツール

プラグインが提供するツールの仕様です。版と状態を返す `ping`、図面を**読み取るのみ**のツール（`layers`・`classes`・
`layer_objects`・`object_counts`）、図面を保存して閉じるツール（`documents`・`save`・`close`）、Vectorworks を終了させる
`quit` があります。更新はツールではなく CLI の `vw2026 install` で行います
（[インストール](install-and-update.md#インストールと更新vw2026-install)）。

**SDK 呼び出しは、SDK リファレンスで挙動が確認されているもののみを使用します**（無いものはリファレンスで
調査してから追加する。[SDK の調査](../sdk-research.md)）。

## 一覧

| ツール | 種類 | 引数 | 結果 |
| --- | --- | --- | --- |
| `@tools` | （予約。`vw2026 tools` が呼び出す） | なし | [通信規則「予約ツール」](../protocol.md#予約ツール) |
| `ping` | read | なし | 下記 |
| `layers` | read | `include_sheets`（既定 true） | `{"layers":[{"name","kind":"design"\|"sheet","referenced","scale","objects","current"}],"count"}`（[読み取りツール](#読み取りツールlayersclasseslayer_objectsobject_counts)） |
| `classes` | read | なし | `{"classes":[名前…],"count"}`（同上） |
| `layer_objects` | read | `layer`（必須）・`limit`（既定 50）・`offset`（既定 0）・`type`（種別の番号。省略＝全ての種別） | `{"layer","unit":"mm","objects":[{"index","type","type_name","pio_name","name","class","bounds":{"left","right","top","bottom"}}],"returned","total"}`（同上） |
| `object_counts` | read | `layer`（省略＝図面全体） | `{"layer"?,"types":[{"type","type_name","count"}],"total"}`（種別番号の昇順。同上） |
| `documents` | read | なし | [図面を操作するツール](#図面を操作するツールdocumentssaveclose) |
| `save` | app | `document`（必須）・`to`（必須） | 同上 |
| `close` | app | `document`（必須） | 同上 |
| `quit` | app | なし | `{"quitting":true}`。図面が開いていれば `internal` で失敗し、`detail` に一覧（下記） |

## 共通の規則

引数は、省略可能なものに既定値を設けます（表の「既定」）。未知の引数と、既知の引数の
未知の値（型の相違・定められた綴り以外の値）は `invalid_args` で返します（[通信規則「互換性」](../protocol.md#互換性)）。
引数の値の `null` は省略と見なさず、型の相違として拒否します（省略する場合はキーごと省略します）。

**数値は、整数と実数を表記で区別しません**（JSON に区別が無く、`2.0` を `2` と出力する言語も多いため）。
実数の引数はどの表記の数値も受理します。整数の引数は、値が整数であれば表記によらず受理し（`2`・`2.0`・`2e0`）、
小数部のある値（`2.5`）と、範囲外の値は `invalid_args` で拒否します。数値でない値（文字列の `"2"` など）は
型の相違として拒否します。**整数の引数は、ツールごとに範囲を定めていなくても 0 以上 2147483647（`int32` の最大）以下です**
（`offset:1e30` のように整数でも収まらない値を拒否するため）。

- **整数か否かと範囲は、記述された 10 進の値で厳密に判定します**（浮動小数点数に変換してから判定しません）。
  浮動小数点数に変換すると、`2.0000000000000001` は 2 に丸められて受理してしまい、`1e400` は無限大になるためです。
  いずれも拒否します。
- 値で判定するので、次のものは受理します: `-0`・`-0.0`（値は 0。負の値として拒否しません）・`0e99999999`・
  `0e-99999999`・`0.0000000001e10`・1 の後に 0 を 1 万個並べて `e-10000` としたもの（値は 1）・
  `2e0000000000000000000000`（指数の先頭の 0 は JSON の文法が許容します。値は 2）。
- 指数がどれほど大きくても、桁溢れや長時間の処理を生じずに拒否します（`1e999999999999999999` は範囲外、
  `1e-999999999999999999` は小数部があるとして `invalid_args`）。

**結果の実数（`scale`・`bounds` の値）の桁数は、元の値に復元できる最短のものとし、表記は Go の `encoding/json`
に準拠します**（プラグインの綴りを定まった基準に統一し、その基準をフィクスチャで C++ と Go の両側から検証するため）。

- **0 と、`1e-6 ≤ |x| < 1e21` は固定小数点で記述します**（`0`・`100000`・`1500000`・`0.0001`・`0.000001`・`2366.025`）。整数の値は
  小数点を付けません（`50.0` は `50`）。前述の通り、呼び出し元は表記で整数と実数を区別しません。
- それ以外は指数で記述し、指数は符号を付けて先頭の 0 を付けません（`1e+21`・`1e-7`・`1.5e-7`）。
- `-0` も Go と同様に `-0` と記述します（読み手にとっては 0 と同じ値です。Go と綴りを統一するため、符号を省きません）。
- **有限でない値（無限大・NaN）は JSON で表現できないので、そのフィールドを `null` にします**（`bounds` は 4 つの
  いずれかが有限でなければ `bounds` ごと `null`）。
- **実数と綴りの組はフィクスチャのファイル（`tests/fixtures/reals.json`）に格納し**、C++ と Go の両方のテストが読み込みます
  （Go と同じ綴りであることを、片側のテストに記述した期待値のみで決定しないため）。これはツールの結果の規則で通信規則では
  ないので、通信規則のフィクスチャ（`protocol/fixtures`。[通信規則のフィクスチャ](../fixtures.md)）とは区別して配置します。

**引数の検証は次の順に行い、最初に該当したもので返します**（どの `reason` を返すかを定めておくため）。
1. 引数の形式（未知の引数・型・値の範囲・必須の欠落・`save` の `to` の形式。[保存先の検証](save-target.md#to-の形式)）→ `invalid_args`。
2. 図面が無い → `internal`（`no_document`）。**「図面が無い」は、ツールが使用するものの有無で判定します。** 前面の図面を読み取る
   ツール（`layers`・`classes`・`layer_objects`・`object_counts`）は `GetCurrentLayer()` が `nullptr` か否か（`ping` の
   `document_open` と同じ基準）、図面を指定するツール（`save`・`close`）は `GetOpenFilesList` の件数が 0 か否か（指定する図面を
   検索する一覧と同じもの）で判定します。2 つが一致することを確認したのは、起動の直後と全て閉じた後のみです（mac / Windows）（[Findings「Timers and Notifications」の 4 の 8](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Timers%20and%20Notifications.md)）。件数が 1 以上でも前面の
   図面が無い状態があれば、読み取りツールは `no_document` で拒否し、`save`・`close` は指定した図面に作用します（いずれもツールが
   使用するものに適合している）。`documents` は失敗にせず空の一覧を返し、`ping` は失敗せずに `document_open` を偽にします。
3. 引数が指定するもの（`document`・`layer`）が実在しない → `invalid_args`。指定するものが 1 つに定まらない
   （`document` に一致する図面が 2 枚以上 → `internal`（`document_ambiguous`）、`layer` に一致するレイヤが 2 枚以上 →
   `internal`（`layer_ambiguous`））。
4. ツールごとの状態（`save` は[保存先の検証「状態の検証」](save-target.md#状態の検証)の順）→ `internal`。

**ツールが返す失敗の `invalid_args` と `internal` は、次のように区別します**（[通信規則「応答」](../protocol.md#応答idresjson)）。

- **`invalid_args`** は、引数そのものが誤っている場合: 未知の引数・型や値の誤り・必須の欠落、引数が指定するもの
  （`document` の図面・`layer` のレイヤ）が実在しない、
  `save` の `to` の形式（拡張子・使用できない文字など）。
- **`internal`** は、引数は正しく、指定するものも実在するが、それ以外の状態のために実行できない場合: 図面が無い
  （`no_document`）・指定するものが 1 つに定まらない（`save`・`close` の `document_ambiguous`、`layer_objects`・
  `object_counts` の `layer_ambiguous`）・
  図面が開いている（`quit` の `documents_open`）・保存先が既に使用されている（`save` の `to_is_open`・
  `file_exists`。`to` は新規に作成するファイルを指定するので、既に存在することは状態に該当する）・保存先の親のフォルダが無い
  （`parent_missing`）・保存先を検証できない（`to_check_failed`）・図面が閉じなかった（`close` の
  `not_closed`）・SDK の失敗（`save` の `save_failed`）・ツール内部の例外。

呼び出し元が理由で分岐する場合は、`code` ではなく `detail` の `reason` を参照します（`reason` を返す失敗のみ）。

## `ping`

```json
{"version":"abc1234","branch":"main","protocol":7,"pid":4242,"session":false,"document_open":true,"current_layer":"1F"}
```

| フィールド | 値 |
| --- | --- |
| `version` | `VW_BUILD_VERSION`（短い sha。更新が反映されたかを確認する手掛かり） |
| `branch` | `VW_BUILD_BRANCH`（PR のプレリリースをインストールしているかが判別できる） |
| `protocol` | 通信規則の版（[通信規則「版」](../protocol.md#版)） |
| `pid` | 受付処理を行っている Vectorworks のプロセス ID（2 つ起動している場合にどちらが受け付けているかが判別できる） |
| `session` | セッション ID を発行しているか（[通信規則「占有」](../protocol.md#占有セッション)）。`ping` は占有によらず全ての呼び出し元に応答する |
| `document_open` | 図面が開いているか（`GetCurrentLayer()` が `nullptr` でないか。[共通の規則](#共通の規則)の検証の順の 2） |
| `current_layer` | 現在のレイヤの名前。図面が開いていなければ `null` |

図面が無い場合は次のように応答します（`GetCurrentLayer()` は異常終了せずに `nullptr` を返します）。

```json
{"version":"abc1234","branch":"main","protocol":7,"pid":4242,"session":false,"document_open":false,"current_layer":null}
```

例の `protocol` は記述した時点の版です（[通信規則「版」](../protocol.md#版)）。

## 読み取りツール（`layers`・`classes`・`layer_objects`・`object_counts`）

図面を読み取るのみのツールです。使用する SDK 呼び出しと挙動は SDK リファレンスで
確認済みです（[SDK の調査 4](../sdk-research.md#4-ツールが使用する-sdk-呼び出し)）。いずれも前面の図面
（アクティブな図面）を読み取ります。

**共通の取り決め:**

- **図面が開かれていなければ（[共通の規則](#共通の規則)の検証の順の 2）`internal` で失敗し、`detail` に `{"reason":"no_document"}` を付与します。**
  SDK は図面が無くても異常終了せずに空の列を返します（[Findings「Layers and Stories」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Layers%20and%20Stories.md)の「OS タイマーの刻みの中・図面 0 枚」・
  [Findings「Attributes and Classes」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Attributes%20and%20Classes.md)の「タイマーの刻みから呼べる。図面 0 枚でも落ちない」）が、
  空の結果（`object_counts` の `total:0` など）を「内容の無い図面」と区別できなくなるので、失敗にします。
- **`layer` はレイヤの名前で、`layers` の `name` と完全に一致するものを検索します**（リソースの参照で取り込まれた
  レイヤは `NNA#<n>_<元の名前>` の形式のまま渡す）。一致しなければ `invalid_args` で拒否します。一致するレイヤが 2 枚以上
  あれば、いずれも読み取らずに `internal` で失敗し、`detail` に `{"reason":"layer_ambiguous"}` を付与します（Vectorworks は
  同名のレイヤを参照で取り込むことも拒否する（[Findings「Layers and Stories」の「参照で入ってきたレイヤ」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Layers%20and%20Stories.md#参照で入ってきたレイヤ参照の方法で-2-通りの姿になる見分けは-isobjectfromreferencedfilelayer)）ので
  通常は発生しないが、未確認の経路で同名のレイヤが 2 枚になっても、暗黙にどちらかを読み取ることはしない。`document_ambiguous` と同じ扱い）。
  図面が無い場合は、`layer` を検証する前に `no_document` で失敗します。
- **並び順は SDK の列の順で、背面→前面です**（`layers` も `layer_objects` も同じ向き）。画面上の並びに
  合わせて並べ替えることはしません（呼び出し元が並べ替える）。

### `layers`

図面のレイヤ（型が `kLayerNode`（31）のもの）を返します（[Findings「Layers and Stories」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Layers%20and%20Stories.md)の
「レイヤを列挙する」）。

| フィールド | 値 |
| --- | --- |
| `name` | レイヤの名前（`GetObjectName`）。リソースの参照で取り込まれたレイヤは `NNA#<n>_<元の名前>`、レイヤの参照で取り込まれたレイヤは元の名前のまま（下記） |
| `kind` | `design`（`ovLayerType` が 1 か 3）・`sheet`（2）。それ以外の値（Findings で未確認の種類）は `null` |
| `referenced` | 参照で取り込まれたレイヤか（`IsObjectFromReferencedFile(layer)`。参照の方法によらず真。下記） |
| `scale` | 縮尺の分母（`GetLayerScaleN`。1/50 なら `50`、1:1 なら `1`）。SDK は `double_gs` で返すので実数として記述します（[Findings「レイヤの縮尺」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Layers%20and%20Stories.md#レイヤの縮尺setlayerscalen-は黙って効かないことがある)。確認した縮尺はいずれも整数で、整数でない縮尺は未確認）。**シートレイヤは SDK の返す通り常に `1`**。有限でなければ `null`（[共通の規則](#共通の規則)） |
| `objects` | レイヤの直下の図形の数（下記「計数の方法」） |
| `current` | 現在のレイヤか（`GetCurrentLayer()` とハンドルが一致するか） |

- **並びは作成順で、デザインレイヤとシートレイヤが混在します。** オーガナイザはデザインレイヤのみを
  この逆の順（`#` の 1 が最前面）に表示し、シートレイヤはこの順のまま表示します
  （[Findings「Layers and Stories」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Layers%20and%20Stories.md)の「順序」）。
- `include_sheets` が偽なら、`kind` が `sheet` のものを除外します（`kind` が `null` のものは除外しない）。
- **`kind` が `null` のレイヤも除外せずに返し**、`objects` も計数します。`object_counts` の図面全体の和と矛盾しない
  ようにするためです。
- **参照で取り込まれたレイヤも返します**（`referenced` が真）。別の図面を参照すると、参照元のデザインレイヤが同じ列に
  型 31 のまま格納され、**参照の方法によって 2 通りの形態になります**（mac と Windows で同じ。[Findings「Layers and Stories」の
  「参照で入ってきたレイヤ」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Layers%20and%20Stories.md#参照で入ってきたレイヤ参照の方法で-2-通りの姿になる見分けは-isobjectfromreferencedfilelayer)。
  [SDK リファレンス #310](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/310)）。

  | | リソースの参照（`CreateReference`・オーガナイザの既定） | レイヤの参照（オーガナイザで「レイヤを参照」にして、参照レイヤを選択する） |
  | --- | --- | --- |
  | `name` | `NNA#<n>_<元の名前>` | 元の名前のまま |
  | `kind` | `design`（`ovLayerType` が 3） | `design`（`ovLayerType` が 1） |
  | `referenced` | 真 | 真 |
  | `objects` | 0（直下に図形が存在しない） | 参照元の図形の数（直下に存在する） |
  | 列の位置 | 自身のレイヤの後 | 列の先頭（自身のレイヤより前） |
  | `scale` | 参照元の縮尺 | 参照元の縮尺 |

  - **参照由来か否かは `IsObjectFromReferencedFile(layer)` で判定します。** `ovLayerType` の 3 はリソースの参照でのみ設定され、
    レイヤの参照で取り込まれたレイヤは 1 で、自身のレイヤと区別できないためです。
  - **レイヤの参照で取り込まれた図形も計数します**（`objects`・`layer_objects`・`object_counts`）。情報パレットも「全てを選択」で
    これを「N 図形 - ロック状態」と計数し（下記「計数の方法」と同じ数）、図形は参照元の型・クラス・外形のまま参照できるためです。
    参照の内容を除外したい呼び出し元は、`referenced` が真のレイヤを除外します（参照で取り込まれたレイヤの直下の図形は、いずれも参照元の
    ものです。図形単位の目印は返しません）。`object_counts` の図面全体も、これらを含んだ和です。
  - 参照を解除する際に「参照レイヤ: ファイルに残す」を選択すると、レイヤと図形は自身のものになり、`referenced` は偽に
    変わります。

### `classes`

`ForEachClass(false, …)` で、前面の図面のクラスの名前を返します（[Findings「Attributes and Classes」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Attributes%20and%20Classes.md)の
「クラスの一覧は `ForEachClass` で列挙する」）。

- **参照している図面のクラス（`NNA#<n>_…`）は含めません**（`doGuestClasses` は偽）。オーガナイザと同じ構成です。
- **並びはクラスの ID の順（作成順）です。** オーガナイザは名前の順に表示しますが、その並べ方に倣わず、
  呼び出し元が並べ替えます。
- 階層の名前（`-` 区切り）は 1 つのクラスの名前のまま返します（親のクラスは実在しない）。

### `layer_objects`・`object_counts`

**計数の方法:** レイヤの**直下**の図形を計数します。グループ・シンボル・PIO・壁などは内容を計数せずに 1 つとします。
終端（型 0）と undo の置き石（型 90。並べ替え・消去・取り消しで増加し、保存しても消えないものがある）は図形として計数しません。これが情報パレットの「全てを選択」の「N 図形」と一致します
（オーガナイザに数の列は無い。[Findings「Layer Objects」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Layer%20Objects.md)の「まとめ」・2・3）。
グループの内部へ降りる指定は設けません。

- **`object_counts` の図面全体は、`layers`（`include_sheets` が真）が返す全てのレイヤの直下の数の和です。**
  シートレイヤ（ビューポートなど）を含み、シンボルの定義（どのレイヤにも属さない）は含みません。
- **`layer_objects` に `type` を渡すと、その種別の図形のみに絞り込んでから計数します。** `offset` は絞り込んだ後の
  並びで読み飛ばす数、`returned` は返した数、`total` は絞り込んだ後の数です（`offset` と `limit` でページを送れるように）。
- **`index`** は、絞り込んだ後の並び（背面→前面。型 0 と 90 を除く）の中の 0 起点の位置です（`offset` に、返した
  配列の中の位置を加えたもの）。消去・取り消し・並べ替えで変化するので、図形の識別子には使用できません。レイヤ全体の
  前後関係が必要なら、`type` を省略して取得します。
- **`limit` は 1〜1000、`offset` は 0 以上、`type` は 0 以上の整数です。** 範囲外の値と、小数部のある値（`2.5`）は
  `invalid_args` で拒否します（数値の受理の仕方は[共通の規則](#共通の規則)）。`offset` が `total` 以上なら `objects` は空で
  成功します。表に無い番号の `type` も拒否せず、該当する図形が無ければ `total` は 0 です。`limit` に上限を設けるのは、
  1 回の受付処理と応答を過大にしないためです。
- **`type`** は `GetObjectTypeN` の番号です（引数の `type` も番号）。**`type_name`** は、番号から名前を取得する手段が
  SDK に無いので（[Findings「Layer Objects」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Layer%20Objects.md)の 5）、
  プラグインが独自の表（下記）で引きます。表に無い番号は `null` です。
- **`type_name` の表。** 綴りはこの表が真実です。レイヤの直下か図形の内部に出現することを Findings で確認した種別のみを
  掲載します（[Findings「Layer Objects」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Layer%20Objects.md)の 5。番号と定数は `Objs.TDType.h`）。
  検証用の図面で出現したシンボル定義（16）は、どのレイヤにも属さず `layer_objects` に出現しないので掲載しません。綴りは定数の
  名前から先頭の `k` と末尾の `Node` を除いて作成しましたが、決定するのは規則ではなくこの表です。**掲載した綴りは
  変更しません。** 表に行を追加する（`null` だった番号に名前を付ける）ことは互換性のある変更として行えるので、呼び出し元は `null` を
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
- **`name`** は `GetObjectName` の値のままで、名前の無い図形は空文字列（`""`）です（Vectorworks でも空の名前と
  区別が無い）。**`class`** はクラスの名前（`GetObjectClass` → `ClassIDToName`）です。
- **`bounds`** は `GetObjectBounds` の外形で、**単位は常に mm**です。結果の **`unit`** は常に `"mm"` で、
  単位の取り違えを防止するために設けます。値は図面の単位にもレイヤの縮尺にもよらない世界座標の実数です。`GetObjectBounds` が偽を
  返した図形と、4 つの値のいずれかが有限でない図形は `null` です（[共通の規則](#共通の規則)）。偽を返すことを確認したのは
  undo の置き石のみで（[Findings「Layer Objects」の 2](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Layer%20Objects.md#2-列に混ざる図形でないもの)）、置き石は計数する図形から除外してあるので、
  列に残る図形で偽を返すものは未確認です。返した場合に備えて `null` にします。
  - **`left ≤ right`・`bottom ≤ top` です。** `left`・`right` は X の小さい側と大きい側、`bottom`・`top` は Y の
    小さい側と大きい側で、プラグインが SDK の外形から最小値と最大値を取得して格納します（画面の座標のように Y が下向きに
    増加するとは解釈しない）。
  - 回転した図形は外接矩形、3D の図形は平面上の外接矩形です。
  - 文字の大きさは、作成時のレイヤの縮尺で決まります（1:50 のレイヤで作成した文字は 50 倍。
    [Findings「Layer Objects」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Layer%20Objects.md)の 6）。
  - シートレイヤの図形（ビューポートなど）は用紙上の mm です。

## `quit`

CLI はプログラムやスクリプトから使用するので、**保存の確認を表示させない**ことを `quit` の前提とします。
保存済みの図面の未保存の変更は SDK から読み取れず（[Findings「未保存の変更ありは読めない」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#未保存の変更ありは読めない)。
`outSaved` も `fIsInMemoryOnly` も、図面がディスクのファイルに関連付けられているかを表すのみ）、無題の図面は変更が無くても
終了時に保存の確認が表示されます。そこで **`quit` は、図面を 1 枚も開いていない場合のみ受理します**。
開いている図面を閉じておくのは呼び出し元の役割です。

- **Vectorworks が起動を完了するまでは、終了を要求せずに `internal` で失敗します**（`detail` は
  `{"reason":"starting","notification_registered":<真偽>}`。図面の一覧を参照するより先に返す）。`error` は「Vectorworks が起動を
  終えていないので終了しませんでした」を含む固定の文言です。
  - **`notification_registered`** は、起動完了の目印となる通知（下記）を登録できたか否かです。偽なら目印は永久に立たないので、
    起動を待機して再度呼び出しても成功しません（`error` にも「起動を終えたことを知る通知を登録できていません」が付く）。プラグインは
    登録できるまで周期処理ごとに再試行するので、偽が続くのは登録が成功しない場合のみです。呼び出し元は、真なら起動を待機して再度呼び出し、
    偽が続けば再呼び出しを中止して人に通知します。
  - 起動を完了する前に終了を要求すると、mac では、Cinema4D（Cineware）の起動の待機中なら起動画面のまま停止して強制終了でしか
    停止できず、「開発元不明のプラグイン」のダイアログの表示中なら要求が暗黙に破棄されます。いずれも呼び出し元からは失敗が見えません
    （[SDK リファレンス #312](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/312)。[Findings「Timers and Notifications」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Timers%20and%20Notifications.md) の 4 の 9）。
  - プラグインは最初の周期処理（起動画面の段階）から応答するので、`launch` の後に `status` が稼働中と応答し、`ping` が応答しても、
    まだ起動を完了していないことがあります。
  - **起動を完了したか否かは、最初の `kNotifyBeforePendingUpdate` で判別します**（Vectorworks のアイドルから届く通知。起動画面・
    Cineware の起動待ち・「開発元不明のプラグイン」のダイアログの表示中には届かず、ホーム画面が表示された後に初めて届く。その後の
    周期処理から終了を要求すると通常通り終了する。[SDK リファレンス #317](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/317)。同 4 の 9「起動を終えた目印」）。ダイアログの表示中は、
    利用者が「続ける」を押すまで `starting` で拒否され続けます。再度呼び出すか否か・いつまで待機するかは呼び出し元が決定します
    （[設計「CLI のプリミティブ性の維持」](../design.md#cli-のプリミティブ性の維持)）。
  - Windows の起動途中の挙動は計測していません（[#316](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/316)。仮想環境で DirectX を有効にできない）。
    mac と同じ実装にしてあり、起動を完了した後に通知が届いて `quit` が受理されることは Windows の実機で確認しました
    （起動の途中に `starting` で拒否されることは Windows では未確認）。
- **図面が 1 枚でも開いていれば（`GetOpenFilesList` の件数で判定します）、終了を要求せずに `internal` で失敗します。**
  `error` は「図面が開いているので終了しませんでした。先に図面を閉じてください」を含む固定の文言で、
  `code` は通信規則の共通の種別のまま（ツールごとの種別は追加しない。[通信規則「応答」](../protocol.md#応答idresjson)）です。
- **失敗の応答の `detail`** に、開いている図面の一覧を記載します（[通信規則「応答」](../protocol.md#応答idresjson)の
  `detail`。呼び出し元が閉じるべき図面を把握するため）。

  ```json
  {"ok":false,"code":"internal","error":"図面が開いているので終了しませんでした。先に図面を閉じてください","detail":{"reason":"documents_open","documents":[{"name":"plan.vwx","path":"/Users/me/plan.vwx","untitled":false},{"name":"名称未設定 1","path":"/Applications/Vectorworks 2026/名称未設定 1","untitled":true}]}}
  ```

  | フィールド | 値 |
  | --- | --- |
  | `reason` | `documents_open`（`quit` の失敗の理由を表す固定の綴り。呼び出し元はこれで分岐する。起動を完了していない場合は前述の `starting`、閉じた図面が内部に残って後処理できなかった場合は後述の `closed_documents_remain` で、いずれも `documents` を持たない） |
  | `documents[].name` | 図面の名前 |
  | `documents[].path` | 図面のパス（`documents` と同じ。`untitled` が真の図面は「アプリケーションのフォルダ＋名前」（「名称未設定 N」・「<元の名前> コピー 1」など）でファイルとしては存在しない） |
  | `documents[].untitled` | ディスクのファイルに関連付けられていない図面か（`documents` の `untitled` と同じ） |
- **一覧が空でも、`close` で閉じた図面が Vectorworks の内部に残っていれば、終了を要求する前に後処理を行います**
  （[閉じた図面の後処理](#閉じた図面の後処理)）。上限の回数までに後処理が完了しなければ、終了を要求せずに `internal` で失敗し、
  `detail` に `{"reason":"closed_documents_remain"}` を付与します。後処理の後に再度一覧を参照し、図面が開いていれば
  （後処理の間に開かれたもの）、終了を要求せずに `documents_open` で拒否します。
- **図面が 0 枚（残存する図面も無い）なら、保存の確認は表示されません。** 応答を書き込んでから、受付処理の外で
  `CloseAllFilesAndQuitVectorworks(true, false)` を要求します（[構成「受付処理の流れ」](architecture.md#受付処理の流れ)）。
  第 1 引数は常に真です（`quit` は図面を保存するか破棄するかを決定しません。決定するのは呼び出し元で、`save`（新しいパスへ）・
  `close`（変更を破棄する）で事前に処理します）。第 2 引数は常に偽です（下記「再起動の手段は設けません」）。
  検証してから要求するまでの間に利用者が
  図面を開くと、保存の確認が表示されうる競合が残りますが、`quit` は占有の中でのみ呼び出せ、呼び出し元が図面を閉じた直後に
  呼び出す使用方法なので、通常は発生しません。
- **図面を保存して閉じるのは呼び出し元です。** 保存するか破棄するかも呼び出し元が決定します（`quit` に保存・破棄の指定は設けず、
  CLI にも追加しない。[設計](../design.md#cli-のプリミティブ性の維持)）。残したい図面（変更）は `save` で新しいパスへ保存してから、
  全ての図面を[下記の `close`](#図面を操作するツールdocumentssaveclose)で閉じます。`close` は保存の確認を表示せず、未保存の変更を破棄します。
- **`close` で指定できない図面が開いていると、CLI のみでは終了できません。** パスを読み取れなかった図面（`path` が空）と、
  `path` が他の図面と重複する図面（`document_ambiguous`。別々のフォルダにある同名の古い版のファイルを開いたものなど）は
  `close` で閉じられないので、開いている間 `quit` は `documents_open` で拒否され続けます。その図面は利用者が Vectorworks で閉じます
  （呼び出し元が人に依頼する）。
- **占有の中でのみ呼び出せます**（[通信規則「占有」](../protocol.md#占有セッション)。占有していなければ
  `no_session`、他が占有していれば `occupied` で拒否される）。Vectorworks が終了すれば占有も消滅します。
- **終了の要求の処理中は拒否されます**（`busy`。[通信規則「Vectorworks が処理中」](../protocol.md#vectorworks-が処理中)）。
  前述の競合で保存の確認が開いている間に届いた 2 つ目の `quit` も `busy` で拒否されるので、終了の要求は重複しません。
  その間は `ping` と読み取りツールも `busy` で拒否され、`vw2026 tools`・`vw2026 session start`・`vw2026 session end` のみが応答します（表に無いツールは `unknown_tool`）
  （[構成「Vectorworks が処理中」](architecture.md#vectorworks-が処理中)）。
- **再起動の手段は設けません**。再起動するのは呼び出し元で、`session start` で占有を
  取得してから `quit`（`--session` を付ける）・停止の待機・`launch`・起動の待機、を組み合わせます（更新を
  挟む形態を含めて[インストール「処理の流れ」](install-and-update.md#処理の流れ)）。
- 終了を確認するのは呼び出し元の役割です（`vw2026 status` が停止中と応答するまで繰り返す。保存の確認が開いている間の扱いは
  [通信規則「生存の判定」](../protocol.md#生存の判定)）。

## 図面を操作するツール（`documents`・`save`・`close`）

`quit` の前に図面を整理する（残したい図面を新しいパスへ保存する・図面を閉じる）ためのツールです。使用する SDK 呼び出しは SDK
リファレンスで確認済みです（[Findings「指定した図面を保存する・閉じる」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#指定した図面を保存する閉じる前面に依存せず)。
Vectorworks 2026 / mac。Windows も挙動は同じで、異なるのはパスの比較方法。[同「Windows」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#windows振る舞いは-mac-と同じ違うのはパスの比べ方と開ける図面は-8-枚まで)・[SDK リファレンス #283](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/283)）。**`close` は保存の確認を表示せずに閉じ、未保存の変更を破棄します**（利用者の判断。下記）。保存は呼び出し元の責任で、
残したい図面は、`save` で新しいパスへ保存してから `close` します（`save` は上書き保存をしない）。不要な図面は、保存せずに `close` します。
利用者の環境設定によっては、保存の度に保存先の隣の `VW Backup/` に図面と同じ大きさのコピーも作成されます
（大きな図面を繰り返し保存する使用方法では、ディスクの容量を多く消費します）。

**前面の図面（コンテキスト）に依存せず、対象の図面を引数で指定します。** プラグインが自身で開いた図面か否かを
記憶することもしません（どの図面にも使用できる）。

| ツール | 種類 | 引数 | 結果 |
| --- | --- | --- | --- |
| `documents` | read | なし | `{"documents":[{"name","path","untitled","active"}],"count"}` |
| `save` | app | `document`（必須）・`to`（必須） | `{"document","path"}`（下記） |
| `close` | app | `document`（必須） | `{"closed":true}`（閉じた図面が内部に残って後処理できなければ `"closed_documents_remain":true` も） |

```json
{"documents":[{"name":"plan.vwx","path":"/Users/me/plan.vwx","untitled":false,"active":true},{"name":"名称未設定 1","path":"/Applications/Vectorworks 2026/名称未設定 1","untitled":true,"active":false}],"count":2}
{"document":"/Applications/Vectorworks 2026/名称未設定 1","path":"/Users/me/tmp/a.vwx"}
{"closed":true}
```

上から `documents`・無題の図面を `to` へ保存した `save`・`close` の結果です。

- **図面が 1 枚も開いていなければ、`save`・`close` は `internal` で失敗し、`detail` に `{"reason":"no_document"}` を
  付与します**（[共通の規則](#共通の規則)の検証の順の 2。`document` を検証する前に返す）。
  `documents` は失敗にせず、空の一覧を返します。
- **`documents`** は `GetOpenFilesList` の項目を、その順のまま並べます（並べ替えない）。**`count`** はその数です。
  順の意味（開いた順・前面からの順など）は未確認なので、呼び出し元は順に依存せず `active` と `path` で識別します。
- **`name`** は `path` の最後の区切り文字より後ろの部分です（SDK に名前を問い合わせずにパスから生成する）。
- **`active`** は、前面の図面（読み取りツールが読み取る図面）か否かを表します。`GetOpenFilesList` の `fIsActive` をそのまま
  記載します（構造体は [Findings「存在する API」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#存在する-apiヘッダ根拠)、実測の `active` は [同「`GetOpenFilesList` と `fileRef`」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#getopenfileslist-と-fileref)）。真が丁度 1 つか否かは
  未確認です。
- **`untitled`** は、図面がディスクのファイルに関連付けられていないか否か（`GetOpenFilesList` の `fIsInMemoryOnly`）です。
  一度も保存していない図面の他、**古い版・読み取り専用・他の Vectorworks が使用中のファイルを開いた図面**も
  真です（Vectorworks はこれらを元のファイルに関連付かないコピーとして開く。`OpenDocumentPath` で開いても確認なしに
  コピーになる。[Findings「利用者の図面を上書き保存する」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#利用者の図面を上書き保存するダイアログが出る場面と保存先の文字列288)）。
  **Windows で確認したのは読み取り専用と使用中のファイルで、いずれも mac と同じく `untitled` が真です**
  （[Findings「Windows——ほかの Vectorworks が開くファイル」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#windowsほかの-vectorworks-が開くファイルと測れなかった残り291)。#291）。
  **Windows の古い版のファイルと、2 つ目の Vectorworks が開いているファイルは計測できていません**（調査した環境では古い版の
  ファイルを作成できず、2 つ目の Vectorworks を起動できなかった）。プラグインはこれらの場面でも Vectorworks の応答
  （`fIsInMemoryOnly`）に従います。`untitled` にならなくても、`save` は新しいパスへのみ保存し（下記）、`close` も
  保存せずに閉じるので、利用者の古い版のファイルが置き換わることはありません。
  **`untitled` が真の図面の `path` は「アプリケーションのフォルダ＋名前」**で、ファイルとしては存在しません（名前は
  「名称未設定 N」・「<元の名前> コピー 1」・「<元の名前> v2026.vwx」など。N はセッション全体の連番で、開いている間は
  重複しない）。**`untitled` が真の図面は、`path` が他の図面と重複することがあります**（下記。その図面は `save`・
  `close` で指定できない）。呼び出し元が `path` をキーにして一覧を扱うと項目を取りこぼします。
- **`document`** は対象の図面のパスで、`documents` の `path` を渡します。ディスクのファイルに関連付けられた図面同士は、
  パスで一意に定まります（同じファイル名で別のフォルダに保存した 2 枚も識別できる）。`documents` が返した文字列と
  **完全に一致**する図面を検索し（呼び出し元は `documents` の結果をそのまま渡す）、一致しなければ `invalid_args` で拒否します。パスを読み取れなかった図面（`documents` の `path` が空）は指定できず、空の `document` は
  `invalid_args` で拒否します（空の文字列でその図面を閉じない）。
  - **`untitled` が真の図面のパスは、一意とは限りません**（Findings も、無題の図面を指定するなら
    `fileRef` の方が確実であるとしている。[Findings「指定した図面を保存する・閉じる」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#指定した図面を保存する閉じる前面に依存せず)）。
    Vectorworks がアプリケーションのフォルダと名前から生成するので、別々のフォルダにある同名の古い版のファイルを開いた
    2 枚は、同じ `<アプリ>/<名前> v2026.vwx` になりえます（「名称未設定 N」の N は重複しない）。SDK リファレンスでは
    確認しておらず、本プラグインの mac の実機での確認で、古い版の 2 枚は重複し、読み取り専用のファイルを開いたコピー
    （「コピー N」）は重複しないことを観察したのみです。利用者がアプリケーションの
    フォルダに同名のファイルを保存して開いていれば、`untitled` が偽の図面とも重複しえます。**`untitled` によらず、
    一致する図面が 2 枚以上あれば、いずれも保存・閉じることをせずに `internal` で失敗し、`detail` に `{"reason":"document_ambiguous"}`
    を付与します**（指定するものは実在するので `invalid_args` にしない。[共通の規則](#共通の規則)の 3 で、指定するものの
    実在の直後に検証する）。暗黙にいずれか 1 枚に作用させると、呼び出し元の意図しない図面を閉じうるためです。
    呼び出し元は、利用者に一方を閉じてもらうなどして識別できるようにします。
  - **`path` は正規化しません。** SDK が返した文字列をそのまま返します。Windows では、保存時に渡された
    文字列がそのまま残り、区切りのみが `\` に統一されます（ドライブ文字の大小・名前の大小・8.3 の短い名前は修正されない）。
    同じファイルでも、保存時の渡し方次第で別の文字列になります。
- **`save`** は、指定した図面を `to` の新しいパスへ保存します。種類は `app` です（図面の内容は変更せず、ファイルに書き込む。
  [ブリッジ「ツールの一覧」](bridge.md#ツールの一覧)）。`to` は必須で（省略すると `invalid_args`）、既存のファイルは拒否します（下記。[保存先の検証](save-target.md)）。
  `untitled` によらず、どの図面にも使用でき、同じ図面に何度でも使用できます（その度に新しいファイルが作成される）。
  - **上書き保存（図面自身のパスへの保存）は提供しません。** 上書き保存は、自身のファイルが移動・削除された、開いた後で
    読み取り専用になったなどの場面で警告のダイアログを表示し、人が押すまで戻りません
    （[Findings「利用者の図面を上書き保存する」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#利用者の図面を上書き保存するダイアログが出る場面と保存先の文字列288)。#288）。
    事前に検証して拒否するには、OS ごとに書き込めるか否かを判定する手順が必要で（読み取り専用の属性・ロック・ACL・リンク・
    ネットワークの失敗）、それでも検証しきれない場面が残るので、提供しないことで回避します。図面自身のパスは開いている図面の
    パスなので、`to` に渡すと `to_is_open` で拒否されます（下記）。必要になれば、`to` を省略した場合の上書き保存を追加できます
    （現在の呼び出し方は変わらない）。
  - **保存すると、図面のパスは `to` へ移ります**（「名前を付けて保存」と同じ）。元のファイルは書き換わりません。
    その後は、利用者の ⌘S もダイアログを表示せずに `to` のファイルへ保存します。利用者が開いた図面を指定した場合も
    同様で、利用者の ⌘S の保存先が呼び出し元の保存先に変わります。古い版・読み取り専用・使用中のファイルを開いたコピーは
    外見が利用者のファイルそのものなので、利用者が気付きにくくなります。**呼び出し元が指定した図面なので、これを許容します**
    （利用者の判断。CLAUDE.md の 5。`close` が変更を破棄するのと同じ扱い）。呼び出し元は、利用者が開いている図面を `save` したら、
    その図面を閉じるまでを一連の操作にします。
  - **同じ図面を続けて保存する場合は、毎回新しい `to` を渡します**（直前の保存先は開いている図面のパスなので、渡すと
    `to_is_open`）。古いファイルは残り、プラグインも CLI も削除しません（CLAUDE.md の 9）。
  - **`untitled` が真の図面も、他の図面と同じく `to` を必要とします**（`path` はファイルとして存在しないので、そのまま
    保存先に使用できません。上記「`untitled`」）。
  - **保存先のフォルダにファイルを追加できない場合は、Vectorworks がダイアログを表示せずに拒否します**（`save_failed`。書き込みの拒否・
    読み取り専用のボリュームや共有。mac も Windows も。[Findings「新しいパスへの保存で、親のフォルダに書き込めないとき」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#新しいパスへの保存で親のフォルダに書き込めないとき304)。#304）。プラグインは事前に検証しません。
  - **ファイルは追加できるが、保存先のフォルダに Vectorworks のバックアップのフォルダ（`VW Backup`）を作成できない場合は、保存は成功するのに
    OK のみの警告（「バックアップフォルダを作成することができませんでした」）が表示され、押すまで戻りません**（保存の度に表示される。
    サブフォルダの作成のみが拒否されている、同名のファイルが存在する、など）。**プラグインは事前に検証せず、この警告を許容します**
    （利用者の判断）。バックアップを作成するか否か・どこへ作成するかは利用者が環境設定（自動保存）で変更できるので、プラグインが
    判断すると設定と矛盾するためです。通常の保存先では発生しません。表示された場合は人が OK を押し、保存は成功として返ります
    （`GSError` は 0）。
    - **上書き保存を廃止した理由（警告のダイアログで停止する）との相違:** 上書き保存の警告は、利用者が図面のファイルを
      移動した・読み取り専用にしたなど、通常起こりうる場面で表示され、保存も成功しないことがありました。この警告は保存は成功し、
      バックアップのフォルダを作成できない特殊な保存先でのみ表示され、表示されるか否かは利用者の環境設定で変わります。
    - **警告が表示されている間は、ダイアログを表示するツールと同じく人が応答するまで戻りません**（[通信規則「予約ツール」](../protocol.md#予約ツール)の「長く掛かるツール」）。Windows では、呼び出したタイマー自身が再入して
      受付処理が入れ子になり、他の要求は全て `busy` で拒否されます。mac ではタイマーが再入せず、何も応答しません
      （[構成「Vectorworks が処理中」](architecture.md#vectorworks-が処理中)）。`save` を呼び出した `vw2026 call` は、既定の待機の上限
      （30 秒）を超えると「実行されたかもしれない」（終了コード 4）で終了し、応答を読み取れません。そうした保存先に保存しうる
      場合は長い `--timeout` を付けるか、警告が閉じる（他の要求が `busy`・無応答でなくなる）のを待機してから `documents` で
      保存されたか否かを確認します（警告が開いている間は `documents` も拒否されるか応答しないので、「保存されなかった」と解釈しない）。
  - **結果の `document`** は、引数の `document` をそのまま返したもの（保存する前の図面のパス）です。
  - **結果の `path` は、保存の後に `GetOpenFilesList` から読み直した文字列です**（`to` をそのまま返さない。渡した文字列がそのまま
    一覧に残る。[Findings「Windows」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#windows振る舞いは-mac-と同じ違うのはパスの比べ方と開ける図面は-8-枚まで)・
    [同「すでに開かれています」の比べ方（mac）](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#このファイルはすでに開かれていますの比べ方macwindows-と同じ文字列の完全一致)）。
  - **`to` は、保存を呼び出す前に形式と状態を検証します**（規則と理由は[保存先の検証](save-target.md)が真実）。
    - **形式**（`invalid_args`）: 絶対パスで、拡張子が `.vwx` であること。`.`・`..`・空の段・制御文字、OS ごとの
      使用できない文字と長さの上限を超えるものも拒否します（規則は[保存先の検証「`to` の形式」](save-target.md#to-の形式)）。
      プラグインは正規化して渡すことはせず、拒否します。
    - **状態**（`internal`）: 次の順に検証し、最初に該当したものの `reason` を `detail` に付与します。
      1. `to_is_open`: `to` が開いている図面のパスと一致する（NFC に正規化し、大文字と小文字を区別せずに比較する）。
      2. `file_exists`: `to` が既にディスクに存在する（上書きしたければ、呼び出し元が先にファイルを削除する）。
      3. `parent_missing`: `to` の親のフォルダが無い（フォルダでない）。
      - 2・3 で OS の呼び出しが「無い」以外の理由で失敗したら `to_check_failed`
        （`{"reason":"to_check_failed","check":"to"|"parent","os_error":<OS のエラーの番号>}`）。
    - **ネットワークの場所の保存先では、サーバーに到達できないと、OS が失敗を返すまで Vectorworks が停止します。**
      そうした保存先には長い `--timeout` を付けます（[保存先の検証「ネットワークの場所」](save-target.md#ネットワークの場所)）。
  - **`SaveActiveDocumentPath` が 0 以外を返したら、`internal` で失敗し、`detail` に
    `{"reason":"save_failed","gs_error":<値>}` を付与します。** `GSError` は SDK のエラーの番号（`Uint32`）で、値ごとの
    意味は SDK に記述されていません（実測で出現したのは 50331649＝`0x03000001` のみで、保存先が既に存在する場合も長すぎる場合も
    同じ値）。そのため理由を値で区別せず、値をそのまま記載します。
  - **保存そのものは取り消されません。** 但し、**図面に PIO を初めて追加した後の最初の保存**は、取り消しの履歴に
    空の段を 1 つ積みます（Vectorworks の挙動で、別名保存も上書き保存も、mac も Windows も同じ。利用者がその後取り消しを押すと、1 回目は
    何も戻らず、2 回目から戻る。履歴は消えない）。それ以外の `save` は積みません。Vectorworks の挙動で、プラグインからは
    回避できません（[Findings「Undo」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Undo.md)の
    「保存はいつも段を積むわけではない」。#288）。
  - **指定した図面を前面にしてから保存し（`SwitchToOpenFile` → `SaveActiveDocumentPath`）、保存を呼び出した後は成否に
    よらず（`save_failed` でも、例外でも）、前面の図面とカレントレイヤを保存の前の状態に戻します**
    （[Findings「指定した図面を保存する・閉じる」の手順](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#手順getopenfileslist-で引きswitchtoopenfile-で前へ出し前面を戻す)）。
    前面にできたか否かは切り替えた後の一覧で確認し、できなければ保存を呼び出さずに前面を戻し、`internal`（`reason` なし）で
    失敗します（前面が変わらないまま保存を呼び出すと、利用者が前面に開いている別の図面を `to` へ保存してしまうため）。
    保存が 0 を返したのに直後の一覧に前面の項目が無い場合も `internal`（`reason` なし）で失敗します（保存は完了しているので、
    呼び出し元は `documents` で確認する）。保存を呼び出す前に返す失敗（`invalid_args`・`no_document`・`to_is_open`・`file_exists`・
    `parent_missing`・`to_check_failed`）では、前面は変わりません。
- **`close`** は対象の図面を閉じます。**保存はせず、未保存の変更は確認なしに破棄します。** 種類は `app` です。
  対象の図面を前面にしてから `CloseDocument()` を呼び出します（前面の図面に作用する。
  [Findings「`CloseDocument()` は `false` を返しながら閉じている」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#closedocument-は-false-を返しながら閉じている未保存の変更も黙って捨てる)）。
  - **保存の確認は表示しません**（利用者の判断）。メニューの「閉じる」（`DoMenuName("Close", 0)`）は確認を表示し、人が
    応答するまで戻らないうえ、何も変更していない図面でも表示される場面があるので使用しません。`close` を呼び出すことを、呼び出し元が
    変更を破棄すると指定したものと見なします（CLAUDE.md の 5）。**利用者が開いている図面を指定すると、その図面の未保存の
    変更も暗黙に失われます。** 保存済みの図面に未保存の変更があるか否かは SDK から事前に読み取れないので、プラグインは
    検証しません。呼び出し元は `documents` の `path` をそのまま渡し、残したい変更は事前に利用者に保存してもらいます。
  - **mac でも Windows でも、どの図面でも確認は表示されません**（無題・保存済み・`OpenDocumentPath` で開いたのみ・古い版や
    読み取り専用・使用中のファイルを開いたコピー・大きな図面。[Findings「Windows と、「閉じる」で確認が出る図面」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#windows-と閉じるで確認が出る図面305mac-も)。#305）。
  - **戻り値（成功しても `false`）では成否が判別できません。** 呼び出しの後に `GetOpenFilesList` を読み取り、対象の図面が
    残っていれば（`document` と完全に一致するパスが残っていれば）`internal` で失敗し、`detail` に `{"reason":"not_closed"}` を付与します。
    前面にできなければ、`save` と同じく閉じずに前面を戻し、`internal`（`reason` なし）で失敗します（利用者の別の図面を
    変更を破棄して閉じないため）。
  - **`close` も `save` も、呼び出しの中で完了してから戻ります**（大きな図面でも。戻った直後の `GetOpenFilesList` は
    最終の状態で、保存したファイルも書き込みが完了している。[Findings「大きな図面でも、呼び出しの中で終わる」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#大きな図面でも呼び出しの中で終わる290macwindows-は下の節実務の図面は測っていない)・
    [SDK リファレンス #290](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/290)）。そのため、戻った直後の一覧で成否を判定します。
    - **処理中は何も応答しません**（`busy` も返せない）。ダイアログが表示されない限りタイマーは再入しないので、mac でも
      Windows でも受付処理の入れ子は発生せず、要求はスプールに残ります。
    - **描画した後の大きな図面は、閉じるのに秒単位の時間を要します**（描画の前は 1 秒未満。所要時間は [Findings「Windows と、「閉じる」で確認が出る図面」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#windows-と閉じるで確認が出る図面305mac-も)）。
      実務の図面（参照・レンダリングしたビューポートなど）は計測していないので、長時間を要する前提で使用します。
    - **呼び出し元の待機の上限を超えても、プラグインは処理を継続して図面を閉じ・保存します。** 呼び出し元は待機しきれずに終了する
      （失敗とは限らない）ので、`documents` で確認します。
    - **Windows では呼び出しが数秒を超えると、Vectorworks が「応答なし」と表示されます。** 図面は破損していないので、失敗と
      解釈しません。
  - **最後の図面を閉じたら（閉じた後の一覧が空なら）、内部に残った図面を後処理します**
    （[閉じた図面の後処理](#閉じた図面の後処理)）。後処理が完了しなくても、指定した図面は閉じたので失敗にせず、結果に
    `"closed_documents_remain":true` を追加して通知します（`{"closed":true,"closed_documents_remain":true}`。後処理が完了した場合・
    後処理が不要だった場合は記載しない）。呼び出し元は、利用者がメニューで終了する前に `quit` で終了するか、人に通知します
    （`quit` は終了を要求する前に再度後処理を行い、完了しなければ拒否する）。
  - 閉じた後に前面になるのは、最後に開いた図面です（mac も Windows も）。`close` の前の前面の図面が残っていれば、
    その図面へ戻します（カレントレイヤも戻る。前面を閉じた場合は戻せない。[Findings「Windows と、「閉じる」で確認が出る図面」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#windows-と閉じるで確認が出る図面305mac-も)）。

## 閉じた図面の後処理

`CloseDocument()` は、先に開いた図面を後に開いた図面が開いている間に閉じると、閉じた図面を Vectorworks の内部に
残します（以下「残存図面」。[Findings「閉じた図面が残り、終了で保存の確認が出る」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Documents.md#閉じた図面が残り終了で保存の確認が出る321323macwindows)。
[SDK リファレンス #321](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/321)）。残存図面は `GetOpenFilesList` に出現せず（`documents` も空）、一覧が空になった
後の終了（`quit` も、利用者のメニューの「終了」も）で、最上位に残った図面が「変更あり」（無題なら変更が無くても）なら
保存の確認が表示され、人が応答するまで停止します。`close` と `quit` は、これを次のように後処理します。

- **判別方法と後処理の方法。** 一覧が空なのに `GetActiveDocument` が真なら残存しています。`GetActiveDocument` が偽になるまで
  `CloseDocument()` で 1 枚ずつ削除します（確認は表示されません。残存図面は `close` が変更を破棄すると指定されて閉じたものなので、
  変更を破棄します）。回数の上限は 64 回です。
- **`close` は、最後の図面を閉じた場合（閉じた後の一覧が空の場合）に後処理を行います**（利用者がメニューで終了しても
  確認が表示されないように）。後処理が完了しなければ、成功のまま結果で通知します（[`close`](#図面を操作するツールdocumentssaveclose)）。
- **`quit` は、終了を要求する前に後処理を行います。** `close` 側の後処理が及ばない経路への備えです（他のプラグインやスクリプトの
  `CloseDocument()` が最後の図面を閉じた・`close` の後処理が上限で停止した、など）。これを除くと、`quit` が
  `{"quitting":true}` を返した後に不可視の図面の保存の確認が表示され、人が応答するまで Vectorworks が停止します。残存
  図面が無ければ `GetActiveDocument` を 1 回呼び出すのみです。後処理が完了しなければ拒否します（[`quit`](#quit)）。
- **図面が開いている間は後処理を行いません**（`CloseDocument()` は開いている図面を閉じてしまう）。他の図面が開いている間に
  `close` した図面は、最後の図面が閉じるまで内部に残ります。最後の図面を `close` 以外（利用者の UI・他のプラグイン）で
  閉じた場合は、プラグインは後処理を行いません（自発的に動作しない。CLAUDE.md の 5）。残存していても、`quit` で終了すれば `quit` が
  後処理を行います。最後の図面を利用者が UI で閉じると、残存図面も消滅します（Windows（Vectorworks 2025）。mac は推定。同じ節）。
- **閉じる順の工夫（後に開いたものから閉じる）では回避しません。** 残存するか否かを決める図面の並びは SDK から読み取れないためです。
- **残存図面がある状態で `SaveActiveDocumentPath` を呼び出すと Vectorworks が異常終了します**（同じ節）。`save` は一覧が空なら
  `no_document` で拒否し、保存を呼び出さないので該当しません。
- **Windows も同様に後処理を行います**（Vectorworks 2025 の Windows でも図面が残存し、この後処理で確認が表示されなくなる。同じ節。
  Vectorworks 2026 の Windows は計測していない。[#323](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/323)）。

