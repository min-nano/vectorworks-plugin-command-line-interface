# 受付処理（ブリッジ）

受け渡しの形式は[通信規則](../protocol.md)が真実であり、本ページはプラグイン側の構造を記述します。各ファイルの
詳細な手順は、そのファイルの先頭と関数のコメントに記述しています。

## 分割の方針

| ファイル | SDK | 内容 |
| --- | --- | --- |
| `src/core/Protocol.h` | 依らない | 通信規則の定数と id の検査。`protocol/fixtures/constants.json` と対（[通信規則のフィクスチャ](../fixtures.md)） |
| `src/core/Json.{h,cpp}` | 依らない | 小規模な JSON の実装。読み取りの挙動は Go の `encoding/json` に合わせる（[通信規則「要求」](../protocol.md#要求idreqjson)）。実数の書き出しの規則は[ツール「共通の規則」](tools.md#共通の規則) |
| `src/core/Files.{h,cpp}`・`FilesPosix.cpp`・`FilesWindows.cpp` | 依らない | スプールに使用する OS の呼び出し（ロック・待機標識・読み書き・自身のモジュールの場所）。開き方の規則は[通信規則「ファイル」](../protocol.md#ファイル)と[「プラグイン側の義務」](../protocol.md#プラグイン側の義務) |
| `src/core/WindowsPath.{h,cpp}` | 依らない | Windows のパスに `\\?\` を付加する変換と、UTF-8 と UTF-16 の変換。WIN32 の場合のみビルドし、スプールと `save` の保存先の検証が共に使用する |
| `src/core/SaveTarget.{h,cpp}`・`SaveTargetPosix.cpp`・`SaveTargetWindows.cpp` | 依らない | `save` の保存先の検証（[保存先の検証](save-target.md)） |
| `src/core/PathCompare.h`・`PathCompareMac.cpp`・`PathCompareWindows.cpp` | 依らない | `save` の `to` と開いている図面のパスを比較する OS の呼び出し。APPLE と WIN32 の場合のみビルドする（[保存先の検証「パスの一致の判定」](save-target.md#パスの一致の判定)） |
| `src/core/Bridge.{h,cpp}` | 依らない | スプール 1 つの操作と、モジュールのパスからスプールの場所を算出する処理（[スプールの準備](#スプールの準備)） |
| `src/core/Serve.{h,cpp}` | 依らない | 1 回の受付処理の手順と占有のセッション ID（[1 回の受付処理](#1-回の受付処理coreserverserve)） |
| `src/core/Args.{h,cpp}` | 依らない | ツールの引数の読み取り方と、失敗の `detail` に `reason` を付加するインタフェース（[ツール「共通の規則」](tools.md#共通の規則)） |
| `src/core/Ping`・`ReadTools`・`DocumentTools`・`Quit`（`.{h,cpp}`） | 依らない | ツールの処理内容の内、SDK に依らない部分（一覧の行 `ToolSpec`・引数の検査・検証の順序・結果の形式）。SDK から読み取る値と SDK の呼び出しは、ツールごとのインタフェース（`Drawing`・`Documents`）で受け取る |
| `src/core/Lifecycle.h` | 依らない | 起動の完了を示すフラグと終了の依頼の処理中を示すフラグ（`core::Lifecycle`）と、1 回の周期処理の手順（`core::tick`） |
| `src/tools/ToolTable.cpp` | 依る | ツールの一覧 |
| `src/tools/<ツール>.cpp`・`Text.h` | 依る | ツールの処理内容の内、SDK を呼び出す部分（`Drawing`・`Documents` を SDK で生成する）と、`TXString` を UTF-8 に変換する `utf8` |
| `src/Clock.cpp` | 依る | OS タイマー・通知の登録・終了の呼び出し（[構成「エントリポイント」](architecture.md#エントリポイント)） |

- **SDK を呼び出す処理と、その前後の手順を分離します。** 手順は `src/core` に配置し、SDK の呼び出しは
  引数（ツールごとのインタフェース・終了の呼び出し）で受け取って、無 SDK のテストで検証します。起動と
  終了のフラグを `Lifecycle.h` に配置するのも同じ理由であり、保存の確認が開く競合（`quit` が図面を確認してから
  終了を要求するまでに図面が開く）と、その間に周期処理が再入すること（Windows）は実機の確認で再現できず、
  無 SDK のテストでのみ検証できるためです。
- **OS ごとのファイルには OS の呼び出しのみを配置します。** パスを文字列として構築・分割する
  処理は `Files.cpp` に 1 つだけ記述し、OS ごとの差異は `Files.h` の定数と `isSeparator` の中で区別します。
- **受付処理の手順は擬似ツールの一覧でテストします**（`tests/ServeTests.cpp`）。表現できるものは
  [通信規則のフィクスチャ](../fixtures.md)に配置し、CLI の擬似プラグイン（`cli/internal/fakeplugin`）も
  同じフィクスチャで検証します。

## スプールの準備

- 場所は `<Plug-Ins>/cli/spool`（[通信規則「スプールの場所」](../protocol.md#スプールの場所)）。
  自身のモジュールの場所から算出し、環境変数は読み取らず、**探索しません**。算出できなければ、次の
  周期処理で再試行します。
- `prepare`: 存在しなければ作成します。失敗した場合は次の回に再試行します（理由は受付処理の結果に含める）。
- `lock`: `bridge.lock` を取得し、取得できたら終了まで解放しません（取得の方法と、拒否された `bridge.lock` を
  1 度だけ再作成することは[通信規則「ロック」](../protocol.md#ロックbridgelock)）。
- `sweep`: **ロックを取得した直後に 1 度だけ**、待機標識を取得できる・存在しない id のファイルを削除します
  （[通信規則「ロック」](../protocol.md#ロックbridgelock)）。

## 1 回の受付処理（`core::Server::serve`）

`serve` はツールの一覧・時刻と、終了の依頼の処理中か否か（`core::Activity`。[構成「Vectorworks が処理中」](architecture.md#vectorworks-が処理中)）
と、起動の状態（`core::Startup`。ツールに `ToolContext::startup` で渡す。[ツール「quit」](tools.md#quit)）を
受け取ります。いずれもタイマーが `core::Lifecycle` から渡します（`core::tick`）。

0. **`serve` の途中で再び呼び出された場合（受付処理の入れ子）**、外側の回の状態に触れず、取り出せる要求を
   全て `busy` で拒否して戻ります。解析もツールの実行も行いません（[通信規則「Vectorworks が処理中」](../protocol.md#vectorworks-が処理中)）。
1. ロックを取得していなければ準備（`prepare` → `lock` → `sweep`）を行います。**ロックを取得できなければ
   何もせずに戻ります**（結果は `waiting`）。
2. 回の始めに列挙した要求を名前の昇順で取り出し、読み取れたら削除し、削除できたものについて待機標識を試行します
   （[通信規則「待機標識」](../protocol.md#待機標識idwait)・[「プラグイン側の義務」](../protocol.md#プラグイン側の義務)）。
3. 読み取れたが破損している要求には `invalid_request` で応答します（[通信規則「要求」](../protocol.md#要求idreqjson)）。
4. 要求ごとに[通信規則「占有」](../protocol.md#占有セッション)の表と「Vectorworks が処理中」に照合し、
   通過すればツールを実行して応答を書き込みます。セッション ID は `core::Server` がメモリ上にのみ保持します。
   ツールが成功して終了の依頼（`ToolStatus::quit`）を設定し、応答を書き込めたら、**その回の残りの要求を
   取り出さずに戻ります**（終了すると応答できないため、残りは配置したままにする）。
5. 結果（`ServeResult`。受付処理の状態 `serving` / `waiting` / `error` と、終了の依頼の有無）を
   返します。タイマーが参照するのは終了の依頼のみで、終了はタイマーが `serve` から戻ってから要求します
   （[構成「受付処理の流れ」](architecture.md#受付処理の流れ)）。

- **セッション ID の発行・終了と終了の依頼は、応答を書き込めた場合にのみ反映します**（[通信規則「占有」](../protocol.md#占有セッション)）。
- **要求 1 件の処理（解析・占有・ツール）の例外は `serve` が捕捉して `internal` で応答します**（ブリッジを
  停止させない）。`serve` 自体が送出した例外は、呼び出し元のタイマー（`Clock`）が捕捉し、Vectorworks へ伝播させません。
- 解析は要求の大きさに対して線形の時間で終了します。1 MiB の要求で 1 回の受付処理が長引き、図面の
  操作を妨げないためです。

## 同一のスプールを参照する 2 つの Vectorworks

Windows では Vectorworks 2026 を 2 つ起動でき、どちらのプラグインも同じスプールを使用します。
**先にロックを取得した方が受付処理を行い、後から起動した方は毎回ロックの取得を試みて失敗するのみです**（呼び出し元から見た扱いは[通信規則「ロック」](../protocol.md#ロックbridgelock)）。

| 局面 | 発生すること |
| --- | --- |
| 2 つ目の起動 | ロックを取得できないので何もしない（`sweep` も行わない） |
| 同時に起動 | ロックはいずれか一方のみが取得できる |
| 受付処理を行っている側がダイアログの表示中など | ロックは保持したままなので引き継がない。終了すれば受付処理に戻る |
| 受付処理を行っている側が終了・異常終了 | OS がロックを解放する。もう一方が次の回にロックを取得し、待機する者が存在しない残骸を削除して引き継ぐ |
| 受付処理を行っていない側が終了 | 何も起きない |

- 前の側が受付処理を行っていた間に配置されて処理されなかった要求は、呼び出し元が待機していれば残存し、
  引き継いだ側が処理します。前の側が書き込んで呼び出し元がまだ読み取っていない応答も残存します。

## ツールの一覧

ツールの一覧の 1 行は `core::Tool`（`ToolSpec` の名前・説明・`inputSchema`・種類 `ToolKind` と、処理内容の
`ToolFn`）です。型と各フィールドの意味は `src/core/Serve.h` に記述しています。

- **ツールを追加する際に変更するのは一覧の 1 行と処理内容 1 つのみ**です。CLI はツールを認識しないので修正しません。
  名前・説明・`inputSchema`・種類は `src/core` の `ToolSpec`（`Ping.h`・`ReadTools.h`・`DocumentTools.h`・`Quit.h`）に配置し、一覧は
  `core::toolOf(spec, &中身)` で構築します（種類を無 SDK のテストで検証するため）。
- `@tools` の結果の形式と `kind` の意味は[通信規則「予約ツール」](../protocol.md#予約ツール)に記述しています。
  予約ツールの内 `@tools`・`@session_start`・`@session_end` は一覧に含めず、`ping` と `quit` は一覧に含めます。
- ツールの結果は JSON のオブジェクトで返します（[通信規則「応答」](../protocol.md#応答idresjson)）。
  返す値が無ければ `Json()` でよく、`Server` が `{}` に変換します。
- ツールが受付処理の状態を知るのは `ToolContext` からのみです（`ping` の `session`・`quit` が参照する起動の状態の
  `startup`。[ツール「quit」](tools.md#quit)）。
- 引数の検査はツールの中で行います（JSON Schema の検証器は保持しない）。省略可能な引数には
  既定値を設け（`inputSchema` の `default` にも記述する）、未知の引数・既知の引数の未知の値は
  `invalid_args` で返します（[通信規則「互換性」](../protocol.md#互換性)）。
- ツールが返す失敗の種別は `invalid_args` と `internal` のみです（区別の方針は[ツール「共通の規則」](tools.md#共通の規則)）。
  他の `code` は `core::Server::serve` が付加します。呼び出し元が機械的に読み取る原因は `ToolStatus::detail` に
  記述します（格納の規則は[通信規則「応答」](../protocol.md#応答idresjson)）。
- **種類 `write` のツールは、undo の手順に対応するまで一覧に含めません。**
- **種類 `write` のツールは、自身で undo の記録を開始し、受付処理から戻る前に必ず終了します**（要求 1 件で
  undo 1 つ）。終了せずに戻ると記録が数十秒開いたままになり、その間の書き込みがそこへ含まれるためです
  （[Findings「Timers and Notifications」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Timers%20and%20Notifications.md) の 7「落とし穴」）。自身で終了する限り、
  他の記録と混在しないので、受付処理は undo の記録が開いているか否かを確認しません
  （[構成「Vectorworks が処理中」](architecture.md#vectorworks-が処理中)）。
