# 実機の確かめ方

プラグインとスクリプトの変更の内、実機でしか確かめられないものの確かめ方です。マージの条件
（利用者が実機で「確認できた」と言うまでマージしない）は `CLAUDE.md`「進め方」にあります。PR には、
確かめ方と、確かめた結果・見られなかった項目を書きます。

## 確かめること

実機の確認は、**道具の結果（応答・図面への作用・描画）が想定どおりか**を確かめるためのものです。
ビルドが通ることと単体テスト・通信規則の見本の照合は CI が保証します（CI は main と合わせた
マージのためのコミットで走る）。そのため、PR のブランチを main に追いつかせることは求めません。
プレリリースは head でビルドするので（[ビルドとリリース「リリースの形」](build-and-release.md#リリースの形)）、
ブランチを作った後の main の変更が**道具の振る舞い・描画に影響しうる**と判断したときだけ、
ブランチに main を取り込んでから確かめます。

実機では `vw2026` で確かめます。ローカルの Claude Code セッションから `vw2026` を呼び、結果の JSON を
読みます。図面に何かが起きたかは人に確かめてもらいます。

## PR のビルドを入れる

PR は、その PR のプレリリースを入れて確かめます（同じ名前のプラグインとして入れ替わる）。タグは
push ごとに変わるので、PR の最新の push のタグを使います。タグは組み立てずに、その PR のプレリリースの名前から
読みます（普通は `dev-<slug>-<PR 番号>-<短い sha>`、slug が空になるブランチ名なら `dev-<PR 番号>-<短い sha>`。
形は[名前と識別子](identifiers.md)の「リリースのタグ」）。

- **入れる**: Vectorworks を終了してから `vw2026 install cli@<その PR のタグ>`（`quit` で終了させても良い。
  終了から起動し直すまでの組み合わせは[インストール「流れ」](install-and-update.md#流れ)）。入ったことは、
  起動し直した後の `vw2026 call ping` の `version`（PR の head の短い sha）と `branch` で確かめます。
- **戻す**: `vw2026 install cli@latest`（main の最新）。
- **`cli` が入っていない計算機と、`install`・初回のスクリプトそのものを変える PR**: その PR のプレリリースの
  初回のスクリプト（`releases/download/<その PR のタグ>/get-vw2026.sh` / `.ps1`）で入れます（スクリプトは、
  取ったリリースのタグを入れる。[インストール「初回のスクリプト」](install-and-update.md#初回のスクリプトget-vw2026sh--ps1)）。
- **zip を展開しただけの `vw2026` では確かめません。** `vw2026` は自分の実行ファイルの場所からスプールを
  求めるので、フォルダの外の CLI は入っている `cli` のスプールを見ません
  （[通信規則「スプールの場所」](../protocol.md#スプールの場所)）。
- **zip を手で取るときは `curl -L` か `gh release download` を使います。** ブラウザで取ると隔離属性（mac）や
  Web からの印（Windows）が付き、アドホック署名のモジュールと CLI が止められて、プラグインと関係のない理由で
  確かめが失敗します（`install` はこれらが付かない前提。[インストール「前提」](install-and-update.md#前提)）。
- **種類 `write`・`app` の道具（`save`・`close`・`quit` など）は、`session start` の印を `--session` に付けて
  呼び、最後に `session end` します。** 印が無いと終了コード 23 で断られます（占有の前後なら `no_session`、
  他が占有している間なら `occupied`）。`VW2026_SESSION` を環境に入れていると、印なしのつもりの呼び出しにも
  印が載るので、印なしの振る舞いを見るときは外します。

## 「開発元不明のプラグイン」のダイアログ

mac でも Windows でも、起動の途中に「開発元不明のプラグイン」のダイアログが出ることがあります
（無効のものがあれば毎回、全部有効なら約 24 時間おき。[構成「拡張機能を登録しない」](architecture.md#拡張機能を登録しない)）。

- `cli` が「無効」と示されていれば、`cli` を選んで「有効化」し、Vectorworks を起動し直します。「有効」と
  示されていれば `cli` は読み込まれています。
- ダイアログの最中に確かめてあるのは図面を開く・作る・書く・保存する・閉じることだけなので、実機の確認は
  ダイアログを「続ける」で閉じてから行います。

## Windows

- 終了コードはプロセスが終わる前にハンドルを開いて読み、落ちた場所は LocalDumps のダンプで見ます
  （[Windows の実機での確かめ方](windows-measurements.md)「確かめ方」）。
- **確かめられるのは描画を伴わない範囲までです。** 用意できる Windows の環境（仮想環境）は DirectX なしでしか
  Vectorworks を起動できないためです（利用者の判断。SDK リファレンスも同じ理由で Windows の測定の一部を
  保留している。[#316](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/316)）。描画を伴う
  項目（図面の表示を目で見て比べるもの・描画の途中で起きることなど）は mac だけで確かめ、Windows で
  見なかった項目は PR に書きます。
- 終了を待つときは 30 秒以上待ちます。Windows は終了の呼び出しが戻ってからプロセスが 28〜30 秒生きて
  ロックを持つためです（[Findings「Timers and Notifications」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Timers%20and%20Notifications.md) の 4 の 9）。
- UNC の保存先は、前もって作った共有（`\\localhost\<共有>\…`）で確かめます。管理共有（`C$`）は昇格して
  いない Vectorworks では拒まれ、プラグインの不具合と見分けが付きません。

## 確認項目

関わる道具・手順を変えたときに、該当する項目をやり直します。載せるのは実機でしか見られないものだけで、
引数の検査の細目など、無 SDK のテストが確かめるものは載せません。道具を足したら、その道具の項目をここに足します。

### 起動と `ping`（どの変更でも）

- `launch` の後 `status` が `running:true` になり、`call ping` の `version` が PR の head の短い sha、`branch` が
  その PR のブランチになる。`vw2026 tools` に道具が並ぶ。
- `launch` の直後から `status` を短い間隔で繰り返し、`running:true` になった直後（起動画面の間）の `call ping` が
  落ちずに応える。
- 図面を 1 枚も開いていないときの `ping` が、`document_open` が偽・`current_layer` が `null` で応える。

### 読む道具（mac と Windows）

- レイヤ・クラス・図形の数と名前を決めた見本の図面（スクリプトで作る）に対して、結果が画面の表示と一致する。
  - レイヤとクラスの顔ぶれ・縮尺はオーガナイザと比べる（並びはオーガナイザと違うので、
    [道具「読む道具」](tools.md#読む道具layersclasseslayer_objectsobject_counts)に書いた向きで比べる）。
  - 数は、レイヤごとに「全てを選択」したときの情報パレットの「N 図形」と比べる（オーガナイザに数の列は無い）。
- 参照で入ったレイヤが、参照の方法の 2 通りとも[道具「`layers`」](tools.md#layers)の表の通りに返る。シートレイヤの
  ビューポートが見本の図面の通りに返る。
- `layer_objects` の `limit`・`offset`・`type` が書いた通りに働き、無いレイヤは `invalid_args` で断られる。
- 図面を 1 枚も開いていないとき、読む道具が `internal`（`no_document`）で失敗する。

### 占有

- 種類 `write`・`app` の道具を印なしで呼ぶと、何も起きずに終了コード 23 で断られる（`session start` の前と
  `session end` の後は `no_session`、占有されている間は `occupied`）。
- Vectorworks を起動し直した後の古い印も `no_session` で断られる。

### `documents`・`save`・`close`（mac と Windows）

- `documents` が開いている図面の一覧と一致する。
- `save` で無題の図面を一時フォルダへ保存できる。前面の図面とそのカレントレイヤが保存の前後で変わらず、結果の
  `path` をそのまま `close` の `document` に渡せる。
- 保存済みの図面も新しい `to` へ保存でき（図面のパスが `to` へ移り、元のファイルはハッシュが変わらない）、同じ図面を
  別の新しい `to` へもう一度保存できる。
- 開いている図面のパスを `to` に渡すと `internal`（`to_is_open`）で断られ、警告のダイアログが出ない。**大文字と
  小文字だけが違う `to` でも `to_is_open` になる**（本物の比べ方が渡っていることの確かめ。mac では NFC と NFD だけが
  違う `to` も見る）。
- 開いていない既存のファイルを `to` に渡すと `internal`（`file_exists`）、無いフォルダの下は `parent_missing`、
  `.vwx` でない `to` と `.`・`..` を含む `to` は `invalid_args` で断られる。
- `close` は、どの図面でも保存の確認を出さずに閉じる（保存した図面・保存していない無題の図面・変更のある保存済みの
  図面。変更は捨てられ、ファイルは変わらない）。閉じた後の一覧に図面が残らない。
- 別々のフォルダにある同じ名前の古い版のファイルを 2 つ開き、`documents` の `path` が重なるときに `save`・`close` が
  `document_ambiguous` で断る。読み取り専用のファイルを開いたコピーも保存の確認を出さずに閉じる（古い版のファイルは、mac の
  「ファイル > 取り出す」で古い版の形式にして作る）。
- 図面を 1 枚も開いていないとき、印を付けた `save`・`close` が `internal`（`no_document`）で失敗する。

### `save` の保存先（Windows）

`CLAUDE.md` の 7 が実機の確認に任せている、CI の環境で作れない場面です
（[保存先の確かめ](save-target.md)を変えたときにやり直す）。

- UNC の `to`（前もって作った共有 `\\localhost\<共有>\…`）へ保存できる。その図面を閉じてから、別の無題の図面で
  同じ UNC へ保存すると `file_exists` で断られる（閉じずに渡すと `to_is_open`）。
- 無い共有（`\\localhost\<無い共有>\a.vwx`）は `parent_missing` で断られる（「無い」の表の 67 を本物の呼び出しで
  確かめる）。
- 無いサーバー（`\\<無いサーバー>\<共有>\a.vwx`。長い `--timeout` を付ける）は `to_check_failed` で断られる
  （`os_error` は「無い」の表に無い番号で、普通は 53。ネットワークによって 1231 などにもなる）。
- `/` 区切りの `to`（`C:/…/a.vwx`）が `invalid_args` で断られる（形の規則が Windows のものになっていることの確かめ）。

### `quit`（mac と Windows）

- `launch` の後 `status` が動いていると答えた直後（ホーム画面が出る前）に `session start` と `call quit` を呼ぶと、
  ダイアログが出ずに `internal`（`starting`）で断られて Vectorworks が起動を続け、ホーム画面が出た後に呼び直すと
  止まる。
- 図面（無題と、保存済みの図面を変更したもの）を開いて `call quit` を呼ぶと、ダイアログが出ずに `internal`
  （`documents_open`）で失敗して `detail` に一覧が載り、`call ping` が応え、`session end` で占有が解ける。
- 図面を全て `close` してから `call quit` を呼ぶと、ダイアログが出ずに `status` が止まっていると答える。
  **閉じる順によらない**（2 枚を開き、先に開いた方を変更して、先に開いたほう・後に開いた方の順に `close` しても、
  保存の確認が出ずに止まる。同じ形の `close` の後に利用者がメニューの「終了」で終えても確認が出ない。
  [道具「閉じた図面の片付け」](tools.md#閉じた図面の片付け)）。
- 更新の組み合わせ（`session start` → `call quit` → 止まるのを待つ → `install` → `launch` → 動き出すのを待つ。
  [インストール「流れ」](install-and-update.md#流れ)）で、`call ping` の `version` が変わる。
- 利用者がメニューの「終了」で終えたときに、落ちずに終わる。

### インストールと初回のスクリプト（mac と Windows）

- その PR のプレリリースの初回のスクリプトを引数なしで実行すると、新しい端末で `vw2026 status` が動き、起動して
  `call ping` の `version` が PR の head の短い sha になり、スプールが `<Plug-Ins>/cli/spool/` にできている。
- Vectorworks が動いている間の `install cli@<同じタグ>` は、「Vectorworks が動いている」終了コードで何も変えない。
- Vectorworks を終了してからの同じタグの `install` は `up_to_date` で終わる（zip を取らず、フックだけが走る）。
  `~/.local/bin/vw2026`（Windows は `Path` の項目）を手で消してから走らせると、元に戻る。
- PR に push して新しいタグができたら、入っている `vw2026` で `install cli@<新しいタグ>` を走らせると `installed` に
  なり（Windows では動いている CLI がよけられて付け替わる）、`vw2026 version` が新しい sha になる。付け替えた
  後の起動で、`cli` が「有効」のまま読み込まれる。
- 初回のスクリプトに `--plugins-dir <場所>` を渡すと既定以外へ入り、そこに入った `vw2026` は指定なしでその場所を
  使う（入った `vw2026` に `--plugins-dir` を渡すと使い方の誤りで断る）。
- 入っている `vw2026` の `uninstall cli` は、何も変えずに終了コード 2 で終わり、初回のスクリプトに `uninstall` を渡す
  コマンドを示す。そのコマンドを走らせると、プラグインのフォルダ（CLI とスプールを含む）・リンクと PATH の項目だけが
  消え、何も残らない。
- 記録の無い `<Plug-Ins>/cli/`（手で置いたもの）には、`install` が断って入れない。
- 確かめた後、`install cli@latest` で main の版に戻り、`call ping` の `version` が main の sha になる。
