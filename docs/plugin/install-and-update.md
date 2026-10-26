# インストール・更新・アンインストール

**配置はすべて `vw2026 install` / `uninstall` が行います。** このプラグイン（`cli`）も、ほかの
プラグインも同じ手順で入ります。何を・どこから取るかは[レジストリとマニフェスト](../registry.md)が
決め、プラグインのフォルダの外に置くもの（`cli` なら CLI と PATH）はそのプラグインのフックが
置きます。最初の 1 回だけは CLI がまだ無いので、取ってきて `install` を走らせるだけの短い
スクリプト（`get-vw2026.sh` / `.ps1`）を使います。

| 段 | 手段 | すること |
| --- | --- | --- |
| 初回 | `get-vw2026.sh` / `.ps1`（curl などで取ってパイプで実行） | 一時フォルダに CLI を取り出し、`vw2026 install cli@<tag>` を走らせる |
| インストール・更新 | `vw2026 install <name>@<tag>` | `<Plug-Ins>/<name>/` を付け替え、フックを走らせる。入っているものと同じタグなら何もしない |
| アンインストール | `vw2026 uninstall <name>` | フックを走らせ、`<Plug-Ins>/<name>/` を取り除く |

```sh
# macOS
curl -fsSL https://github.com/min-nano/vectorworks-plugin-command-line-interface/releases/latest/download/get-vw2026.sh | sh
curl -fsSL …/get-vw2026.sh | sh -s -- --plugins-dir <Plug-Ins>   # 既定以外の場所へ
```

```powershell
# Windows
irm https://github.com/min-nano/vectorworks-plugin-command-line-interface/releases/latest/download/get-vw2026.ps1 | iex
& ([scriptblock]::Create((irm …/get-vw2026.ps1))) --plugins-dir <Plug-Ins>   # 既定以外の場所へ
```

## 前提

- **プラグインのフォルダ（`<Plug-Ins>`）と CLI の場所（`<CLI>`）は同じドライブ（ボリューム）に
  あるものとします。** 既定ではどちらも利用者のホームの下です。入れ替えは名前の変更だけで
  行い、ボリュームをまたぐ場合の扱い（コピーへの切り替え・その検出）は持ちません。名前の
  変更が失敗したら、何も変えずに失敗で終わります。
- **入れた場所を記録しません。** 既定以外の `<Plug-Ins>` を使う利用者は、`install` と
  `uninstall` に毎回 `--plugins-dir`（環境変数 `VW2026_PLUGINS_DIR`）を渡します（[設計「CLI はプリミティブに保つ」](../design.md#cli-はプリミティブに保つ)）。
  入れたものの記録は、プラグインのフォルダの中にだけ置きます（[レジストリ「入れたときの記録」](../registry.md#入れたときの記録)）。
- **安定版と開発版を区別しません。** PR のプレリリースを `cli@<そのタグ>` で入れると、プラグインも
  CLI もそのビルドのものに入れ替わります。
- **隔離属性の解除（mac）・`Unblock-File`（Windows）は行いません。** curl・`Invoke-RestMethod`・
  Go の HTTP で取ったファイルには、隔離属性も Web からの印（Mark of the Web）も付かないためです。
  署名はビルドがかけたアドホック署名で、zip から取り出したファイルにそのまま残ります。
- **同じプラグインを二重に入れないのは利用者の責任です**（[レジストリ「提供側に求めること」](../registry.md#提供側に求めること)）。

場所は[名前と識別子「置き場所」](identifiers.md#置き場所)にあります。

## 初回のスクリプト（`get-vw2026.sh` / `.ps1`）

**CLI を取ってきて `install cli@<tag>` を走らせるだけ**です。配置の手順・PATH・判断は `install` と
`cli` のフックが持ち、スクリプトは持ちません。

1. 引数から `cli@<tag>` の形のものを探す（無ければ `cli@latest` を足す）。ほかの名前は受け付けない
   （このスクリプトは `cli` を入れるためのもの）。
2. ターゲット（`uname -m` / Windows は `windows-x64`）の zip を一時フォルダへ取ってきて、中の
   `bin/vw2026`（Windows は `bin\vw2026.exe`）だけを取り出す。zip の場所は既定のレジストリの `cli` の
   規則と同じ形で決める（`latest` なら `releases/latest/download/cli-<target>.zip`、それ以外は
   `releases/download/<tag>/cli-<target>.zip`）。JSON を読まずに済むように、マニフェストは読まない。
3. 取り出した CLI で `install` を走らせる（スクリプトの引数をそのまま渡す）。
4. 一時フォルダを消し、`install` の終了コードで終わる。「Vectorworks が動いている」終了コード
   （番号は `vw2026 --help`）なら「Vectorworks を終了してからもう一度走らせる」と表示する。

- CLI だけの資産は持ちません。zip から取り出します。
- 一時フォルダの CLI は、`install` の中でマニフェストと zip をもう一度取ります（スクリプトが取った
  zip は渡さない。指定を増やさないため。zip は小さい）。`install` は zip を sha256 で照らします。
- `latest` が、スクリプトが zip を取ってから `install` がマニフェストを取るまでの間に進むと、
  一時フォルダの CLI と入る CLI は別のビルドになります。入れる手順はどのプラグインにも同じ
  （ビルドに固有の置き方は、入るものの側のフックが持つ）なので、一時フォルダの CLI がその
  `manifest_version` を読めれば正しく入ります。
- PR のプレリリースを試すときは、そのリリースのスクリプトに `cli@<そのタグ>` を渡します
  （スクリプトが取る zip もそのタグのものになり、`install` へもそのまま渡る）。

## インストールと更新（`vw2026 install`）

**初回のインストールも更新も、同じ `install` で行います。**

```sh
vw2026 install cli@latest                         # main の最新（同じタグなら何もしない）
vw2026 install cli@dev-feature-x-61-a1b2c3d       # PR のプレリリース
vw2026 install cli@latest --check                 # 入れずに、新しいビルドがあるかだけ返す（動いていてもよい）
vw2026 install cli@latest --reinstall             # 同じタグでも入れ直す（フックも走らせ直す）
vw2026 install cli@latest --plugins-dir <Plug-Ins>
vw2026 install import-ifc-homeskz@latest
vw2026 install my-plugin@2026.3.1 --registry ./plugins.json
```

- **名前とタグは必須です**（`<name>@<tag>`。既定のタグは持たない）。1 回に 1 つだけ入れます
  （途中で失敗したときに、どこまで入ったかを持たないため）。
- `--check` は、新しいビルドがあるか（`available`）と最新か（`up_to_date`）を、終了コードでは
  なく出力の `outcome` で分けます（新しいビルドがあることは失敗ではないため。判定できなかった
  ときだけ失敗）。入っていなければ `available` です。
- `--reinstall` は、同じタグでも付け替えとフックを行います（`cli` の PATH を通し直す・消えた
  `<CLI>/bin/vw2026` を置き直すとき）。
- 指定・環境変数・終了コードの具体的な値は `vw2026 help install` が真実です（`CLAUDE.md`「Go」）。

### 流れ

```
vw2026 install <name>@<tag>
  1. 名前を引く                       既定のレジストリ → --registry の順（レジストリ「レジストリの順」）
  2. マニフェストを取る               合った規則の path（{tag} を置き換える）。tag を照らし、今のターゲットを選ぶ
  3. 入れる先を確かめる               <Plug-Ins>/<name>/ と中の記録（下記「入れる先のフォルダ」）
                                     記録の tag と target が同じなら終わり {"outcome":"up_to_date"}
                                     --check なら入れずに {"outcome":"available"}
  4. Vectorworks が動いていないか     ロックが掴まれていれば終わり {"outcome":"vectorworks_running"}（専用の終了コード）
  5. zip を取って照らす               asset を <CLI>/staging/<name>.zip へ。sha256 が合わなければ失敗
  6. 組み立てる                       <CLI>/staging/<name>/ へ展開し、モジュールを確かめ、記録を書く
  7. プラグインのフォルダを付け替える  下記「入れ替えの手順」
  8. install のフックを走らせる        失敗したら 7 を戻す（レジストリ「フック」）
  9. 後始末                           <CLI>/staging/<name>*・<CLI>/old/<name>/ を消す
                                     {"outcome":"installed","name":…,"requested":…,"tag":…,"target":…,
                                      "registry":…,"plugins_dir":…,"hook":{…}}
```

- **GitHub の API を使いません。** レジストリの規則とマニフェストの絶対の参照で、取るものの場所が
  決まります。API の回数制限・トークンの扱い・リリース本文の読み取りが要りません。
- **新しいかは、タグとターゲットだけで比べます**（タグは使い回さないので、タグが同じならビルドも
  同じ）。マニフェストだけを取り、zip は取りません。
- **プラグインと CLI は 1 つの zip から入ります**（`cli` の zip の `bin/` を、`cli` のフックが
  `<CLI>/bin/` へ置く）。動いているプラグインと入っている CLI は同じビルドです（[通信規則「版」](../protocol.md#版)の前提）。
- 初回でも Vectorworks が動いていれば何もせずに終わり、CLI も置きません（何も変えない）。
- **Vectorworks が動いているかはロックで判定します**（[通信規則「生存の判定」](../protocol.md#生存の判定)）。
  プロセス名は見ません。ロックを掴んでいない Vectorworks（`cli` が入っていない・起動の途中で
  最初の刻みの前・2 つ目の起動）が居るときは、Windows では読み込み中のフォルダの名前を変えられないので
  付け替えが失敗し、何も変わりません。mac では付け替えが成功し、その
  Vectorworks は古い版のまま動き、次の起動から新しい版になります。
- **終了させるのは呼ぶ側です。** `install` は終了を頼まず、待ちもしません（[設計「CLI は
  プリミティブに保つ」](../design.md#cli-はプリミティブに保つ)）。終了から起動し直すまでを 1 度に
  行いたい呼ぶ側は、次のように組み合わせます（`quit` は図面を 1 枚も開いていないときだけ受け付けるので、呼ぶ側が先に図面を閉じる。
  [道具](tools.md#quit)。この組み合わせの真実はここで、ほかのページはここを参照します）。

  ```sh
  id=…  # vw2026 session start の {"session":…}（quit は占有の中でだけ呼べる）
  # 失敗したら占有を残さない。図面が開いているときの失敗は、標準出力の detail に一覧が出る
  # （呼ぶ側は図面を閉じてからやり直す）
  vw2026 call quit --session "$id" || { code=$?; vw2026 session end --session "$id"; exit "$code"; }
  # 止まるのを待つ（5 で止まった。0 のまま上限を過ぎたのは、確かめてから頼むまでの間に
  # 利用者が図面を開いて保存の確認が開き、取り消された（または開いたままの）とき。0 のまま抜けると成功に見えるので、
  # 待ちきれなかったことを表す 3 で抜ける。占有も解く）
  for i in $(seq 120); do
      vw2026 status >/dev/null 2>&1; code=$?
      [ "$code" -ne 0 ] && break
      sleep 1
  done
  case $code in 5) ;; 0) vw2026 session end --session "$id"; exit 3 ;; *) exit "$code" ;; esac
  vw2026 install cli@latest && vw2026 launch || exit
  # 動き出すのを待つ（0 で動き出した。5 のまま上限を過ぎた・1 で失敗したら、その終了コードで抜ける）
  for i in $(seq 120); do
      vw2026 status >/dev/null 2>&1; code=$?
      [ "$code" -ne 5 ] && break
      sleep 1
  done
  exit "$code"
  ```

  CLI は動き出す・止まるのを待つコマンドを持ちません（[設計](../design.md#cli-はプリミティブに保つ)）。
  `vw2026 status` を繰り返し、終了コードで分けます（PowerShell の書き方は `vw2026 help status`）。

  Vectorworks が終われば占有も消えるので、起動し直したあとに使うなら占有を取り直します。

  `quit` が入る（[実装の順序](plan.md)の段 6）までは、`call quit` と止まるのを待つところを、
  利用者が Vectorworks を手で終了することに置き換えます。

- 失敗したら（取れない・照らせない・展開できない・付け替えられない・フックが失敗した）、
  `<Plug-Ins>` を元のままにして理由を `message` に載せ、失敗の終了コードで終わります
  （フックがフォルダの外で変えたものは除く）。

### 入れる先のフォルダ

**フォルダ名はレジストリの名前です**（`<Plug-Ins>/<name>/`。`cli` は `<Plug-Ins>/cli/`）。見れば何の
プラグインか分かり、名前からフォルダが直接決まります。

| `<Plug-Ins>/<name>/` | 振る舞い |
| --- | --- |
| 無い | 作って入れる |
| あり、記録の `name` が一致し、記録の `registry` が今回使ったレジストリと同じ | 更新として付け替える |
| あるが、記録が無い（手で入れたもの・ほかのインストーラで入れたもの） | **何も変えずに失敗にする。** `message` に場所を載せる（利用者のものを上書きしない） |
| あるが、記録の `registry` が違う（後から既定のレジストリに同じ名前が載り、そちらが勝つようになったときなど） | **何も変えずに失敗にする。** 別のプラグインを黙って入れ替えないため。利用者が `uninstall` してから入れ直す |

- **更新ではフォルダのパスが変わりません。** Vectorworks は署名の無いプラグインの「有効化」を
  モジュールのフルパスで覚えるので、モジュールのファイル名が変わらない限り、更新のあとも有効の
  まま読み込まれます（[Findings「Plug-in Modules」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Plug-in%20Modules.md)の「開発元不明のプラグイン」のダイアログ。SDK リファレンス #258）。
  `uninstall` してから入れ直しても同じパスなので、その間に Vectorworks を起動しなければ有効のままです
  （プラグインが居ない起動でダイアログを閉じると、有効化は消える）。
- 同じ名前のフォルダに記録が無ければ断るので、**同じ名前のフォルダへの上書きだけは CLI が防ぎます**。
  別の名前で同じプラグインが入っているか（手で入れたもの・別のレジストリの名前）は見ません。

### 入れ替えの手順

1. **組み立て**: zip を `<CLI>/staging/<name>/` に展開し、直下のモジュールを確かめて、記録
   （`vw2026-install.json`）を書く。前回の残りがあれば先に消す。
2. **付け替え**: `<Plug-Ins>/<name>` → `<CLI>/old/<name>`、続けて `<CLI>/staging/<name>` →
   `<Plug-Ins>/<name>`。1 つ目が失敗したら何もせずに終わる（エクスプローラーやウイルス対策が
   フォルダの中のファイルを開いているとき・Vectorworks が読み込んでいるとき）。2 つ目が
   失敗したら 1 つ目を戻す。まだ入っていなければ 1 つ目は飛ばす。
3. **フック**: `install` のフックを走らせる。失敗したら、`<Plug-Ins>/<name>` を消して
   `<CLI>/old/<name>` を戻す（最初に入れたときは消すだけ）。`<CLI>/data/<name>/` は最初に入れたときに
   作り、更新では残す。

- 付け替えを名前の変更で行うのは、途中で失敗したときに元へ戻せるようにするためです（ファイル
  ごとに置き換えると、半端な版が残る）。
- 組み立てと退避の場所を `<CLI>` に置くのは、`Plug-Ins` の外（Vectorworks に読まれない）で、
  前提により `<Plug-Ins>` と同じドライブにあるためです。
- フックが失敗して元に戻したとき、フックがフォルダの外で変えたもの（`cli` なら `<CLI>/bin/` や PATH）は
  戻りません。フックは何度走っても同じ結果になるように書くので、もう一度 `install` を走らせます。

## `cli` のフック

**このプラグインの配置の知識（CLI の置き方と PATH）は、`cli` のフック（`scripts/hooks/`）の 1 か所に
置き、スクリプトのテストで確かめます**（下記「テスト」）。`install` の Go のコードは、どの
プラグインにも同じ手順だけを持ち、`cli` を特別に扱いません。

| ファイル | すること |
| --- | --- |
| `hooks/install.sh` / `hooks/install.ps1` | 下記「CLI の入れ替え」と「PATH」 |
| `hooks/uninstall.sh` / `hooks/uninstall.ps1` | 下記「`uninstall` のフック」 |
| `hooks/lib.sh` / `hooks/lib.ps1` | 両方のフックが読み込む関数（PATH の項目の比べ方・足し方・除き方、リンクの判定）。`install` と `uninstall` が同じ比べ方を使うように 1 か所に置く |

- zip の直下に `hooks/` として入ります（[ビルド「梱包」](build-and-release.md#梱包)）。
- 標準出力に `{"path":…}`（下記「PATH」）と、Windows で消せなかったものを `{"left":[…]}` で出します。
  `install` / `uninstall` の出力の `hook` に載ります。
- zip の `bin/` はプラグインのフォルダにも残ります（zip の中身はそのまま置くため）。CLI は
  `<CLI>/bin/` のものを使い、プラグインのフォルダの中の CLI は動かしません（Windows では、動いている
  ファイルを含むフォルダの名前を変えられず、次の付け替えが失敗するため）。

### CLI の入れ替え

**`install` のフックは、`<Plug-Ins>/cli/bin/` の CLI を `<CLI>/bin/` へ置きます。** プラグインを
付け替えたあとに走るので、プラグインの入れ替えが失敗したときに CLI だけが新しくなることは
ありません。入れ替える CLI は `install` を走らせている本人で、ほかの呼び出しとしても
動いていることがあります。動いている実行ファイルは、mac では上書きでき（動いているプロセスは
古い inode を使い続ける）、Windows では上書きも削除もできませんが、**名前の変更はできます**。

1. 新しい CLI を `<CLI>/bin/` の中へ一時的な名前（`vw2026.new` / `vw2026.exe.new`）でコピーする。
2. mac: `vw2026.new` を `vw2026` へ名前を変える（上書き）。
   Windows: `vw2026.exe` を `vw2026.exe.old-<時刻>` へ名前を変え、`vw2026.exe.new` を
   `vw2026.exe` へ名前を変える。2 つ目が失敗したら 1 つ目を戻す。
3. 残っている `vw2026.exe.old-*` を消してみる（動いている間は消せないので、失敗は無視して
   次の `install` で消す）。

- 名前を変えられた古い CLI は、動いている間はそのまま動き続けます。次に呼ばれたときから
  新しい CLI です。
- 初回のスクリプトから走らせたときは、動いている CLI は一時フォルダにあるので、`<CLI>/bin/` の
  入れ替えは動いているファイルに当たりません。

### PATH

**`install` のフックのたびに確かめ、足りなければ通します**（何度走らせても同じ結果になる）。結果は出力の
`path`（`linked` / `exists` / `added` / `not_in_path` / `conflict`）に載せます。PATH の失敗で
フックを失敗にはしません（プラグインは動く）。

| OS | すること | 既にあるとき |
| --- | --- | --- |
| mac | `~/.local/bin/vw2026` → `<CLI>/bin/vw2026` のシンボリックリンク（`~/.local/bin` が無ければ作る） | **このプラグインの CLI を指すリンクなら何もしない。** それ以外（利用者のファイル・別の場所を指すリンク）なら触らず `conflict` |
| Windows | 利用者の環境変数 `Path`（`HKCU\Environment`）に `<CLI>\bin` を追加 | 同じ項目が既にあれば何もしない（`exists`） |

- 行き先がプラグインのフォルダの外で版によらず同じなので、更新で張り直す必要はありません。
- **シェルの設定ファイル（`.zshrc` 等）は書き換えません。** `~/.local/bin` が PATH に無ければ
  `not_in_path` とし、追加の方法を標準エラーに表示するだけです。
- 新しい値は **新しく開いた端末から**効きます。

#### Windows の `Path` の読み書き

**`HKCU\Environment` の値 `Path` を、.NET の `Microsoft.Win32.RegistryKey` で直接読み書きします。**

- **展開しない値を読み、型を保って書き戻します。** 利用者の `Path` には `%USERPROFILE%\…` の
  ような項目があり、型は REG_EXPAND_SZ です。`GetValue('Path', $null,
  [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)` で読み、`GetValueKind` で
  型を得て、`SetValue('Path', <値>, <型>)` で書きます。PowerShell の
  `[Environment]::SetEnvironmentVariable('Path', …, 'User')` は展開した値を読み、REG_SZ で書き
  戻すとされるので、この項目が固定の値に変わるおそれがあり、使いません（[設計のレビュー B2](../design-review.md#b2-windows-の-path-の書き換えが既存の項目を壊しうる)）。
  `setx` も 1024 文字で切り詰めるので使いません。
- 型は読んだものをそのまま使います（REG_SZ なら REG_SZ）。値が無ければ REG_EXPAND_SZ で作ります
  （あとから利用者が `%…%` を含む項目を足しても展開されるように）。REG_SZ・REG_EXPAND_SZ 以外の
  型なら書き換えず、出力の `path` を `conflict` にします。
- 項目は `;` で分け、**`%…%` を展開せずに比べます。** 比べる前に、項目の前後の空白を除き、
  全体を囲む `"` を 1 組だけ除き、末尾の `\` を除いて、大文字と小文字を区別せずに比べます
  （Windows が同じディレクトリとして解決する書き方の違いを、同じ項目とみなすため）。
  `install` の「同じ項目が既にある」と `uninstall` の「一致する項目」は**同じ比べ方の関数**
  （`hooks/lib.ps1`）で判定します（片方だけが一致とみなすと、`install` が `exists` と答えた項目を
  `uninstall` が残すことになるため）。
  - `uninstall` は一致する項目を**すべて**除きます（利用者が書いた ` "C:\…\bin"` のような形も含む）。
    どれも `uninstall` が消すディレクトリを指すので、残すと消えたディレクトリを指したままになるため
    です。
  - 展開しないので、`%LOCALAPPDATA%\vectorworks2026-cli\bin` のように書かれた項目は
    `<CLI>\bin` と一致しません。`install` はそれとは別に `<CLI>\bin` を足し、`uninstall` はその
    項目を残します。
- 足すときは、値が空か末尾が `;` なら `<CLI>\bin` を、そうでなければ `;<CLI>\bin` を末尾に付けます
  （空の項目を増やさない）。ほかの項目の並び・空の項目・前後の空白は変えません。
- 除くときは、項目とその前の区切りの `;` を除きます（先頭の項目なら後ろの `;`）。元の値の末尾に
  あった `;` は戻りませんが（`C:\a;` に足して除くと `C:\a`）、空の項目は増えません。
- **除いて空になったら値ごと消します**（`DeleteValue`）。フックは値を作ったかを記録しないので、
  `Path` が無かった利用者で元の状態（値が無い）に戻すためです。元から空の値があった利用者では値が無く
  なりますが、どちらも `PATH` に足す項目が無い点で同じです。
- 書いたあと `user32` の `SendMessageTimeoutW(HWND_BROADCAST, WM_SETTINGCHANGE, 0, "Environment",
  SMTO_ABORTIFHUNG, 1000 ms)` で知らせます（`Add-Type` で呼ぶ）。上限はトップレベルのウィンドウ 1 つずつに順にかかる
  ので、全体で待つ時間は決まりません（応えの遅いウィンドウが多いと、その数だけ延びる）。全体の
  上限は持ちません（途中で打ち切ると、Explorer など後ろのウィンドウに知らせが届かず、そこから
  開いた端末に新しい値が効かないため）。知らせの失敗は無視します（値は書けていて、利用者が
  ログインし直せば効くため）。
- レジストリの読み書きは関数に分け、テストでは差し替えます（下記「テスト」）。実物のレジストリでの
  型の扱いは、下記「実物の exe の入れ替えのジョブ」で確かめます。

### `uninstall` のフック

消すもの: CLI（`<CLI>/bin/`）・スプール（`<CLI>/spool/`）・リンクと PATH の項目。

- 動いている CLI（`uninstall` を走らせている本人）は、mac では消せます。Windows では消せないので
  `vw2026.exe.old-<時刻>` へ名前を変えて残し、出力に `"left":[…]` として載せます（フックは失敗に
  しない）。
- **PATH の後始末**:
  - mac: `~/.local/bin/vw2026` が**シンボリックリンクで、`<CLI>/bin/` の中を指すときだけ**消す。
  - Windows: 利用者の `Path` から、`<CLI>\bin` と**一致する項目だけ**を除く（比べ方と読み書きは
    `install` と同じ。[Windows の `Path` の読み書き](#windows-の-path-の読み書き)）。
- `<CLI>` の下は、決まった名前（`bin` / `spool`）のものだけを消します。`<CLI>` そのものは消しません
  （ほかのプラグインの `data/<name>/` があるかもしれない。空になった `<CLI>` は CLI が消す）。

## アンインストール（`vw2026 uninstall`）

```sh
vw2026 uninstall import-ifc-homeskz
vw2026 uninstall cli                          # CLI そのものも消える
vw2026 uninstall cli --plugins-dir <Plug-Ins> # 既定以外の <Plug-Ins> から
```

1. **Vectorworks が動いていれば何もせずに終わる**（ロックで判定。install と同じ終了コード）。
2. **安全弁**: `<Plug-Ins>/<name>/` に記録があり、記録の `name` がフォルダ名と一致し、記録の
   `module` がフォルダの中に実在するときだけ進む。フォルダが無ければ何もせずに成功
   （`not_installed`）。フォルダはあるが記録が無い・一致しないなら、何も消さずに失敗にする
   （`vw2026` が入れたものではない）。
3. 記録の `hooks.uninstall` を、入っているフォルダで走らせる。失敗したら何も消さずに失敗にする。
4. `<Plug-Ins>/<name>` を `<CLI>/old/<name>` へ名前を変えてから消す（消す途中で失敗しても、
   `Plug-Ins` の中に半端なプラグインを残さないため）。`<CLI>/data/<name>/` を消す。
5. `<CLI>` に `staging` / `old` / `data` の空のディレクトリしか無ければ、`<CLI>` も消す。

- **レジストリを読みません。** 記録だけで決まるので、レジストリから消えたプラグインや、
  ネットワークにつながっていないときでも外せます。
- **`uninstall cli` は、ほかのプラグインが残っていても断りません。** 出力の `remaining` に、記録の
  ある `<Plug-Ins>` の直下のフォルダの名前を載せます。残ったプラグインは Vectorworks の中で動き
  続けますが、`vw2026` が無くなるので外せなくなります（先に外したい呼ぶ側はこの一覧を見る）。
- 出力: `{"outcome":"uninstalled","name":…,"hook":{…},"remaining":[…]}`（`remaining` は `cli` のときだけ）。

## 消すコードの歯止め

**利用者のものを消すコードは次の場所だけ**にし、歯止めを緩めません。段 3 でこの表を `CLAUDE.md` に写します。

| 消すコード | 消してよいもの |
| --- | --- |
| `install`（組み立て・後始末・フックの失敗の戻し） | `<CLI>/staging/<name>`・`<CLI>/staging/<name>.zip`・`<CLI>/old/<name>/`（退避した版は、記録の `name` が一致し、記録の `module` があるときだけ）・フックが失敗したときの新しい `<Plug-Ins>/<name>/`（自分が置いたもの） |
| `uninstall` | 記録の `name` が一致し、記録の `module` がある `<Plug-Ins>/<name>/`・`<CLI>/data/<name>/`・空の `<CLI>` |
| `cli` の `install` のフック | `<CLI>/bin/` の中の `vw2026.exe.old-*`・`vw2026.new` / `vw2026.exe.new` |
| `cli` の `uninstall` のフック | `<CLI>/bin/`・`<CLI>/spool/`・`<CLI>/bin/` の中を指す `~/.local/bin/vw2026`・`Path` の `<CLI>\bin` と一致する項目 |

- ほかのプラグインのフックがフォルダの外で消すものは、そのプラグインの提供側の責任です
  （[レジストリ「フック」](../registry.md#フック)）。

## テスト

- **`install` / `uninstall`**: Go の単体テスト。配置を行うパッケージの関数には、場所
  （`<Plug-Ins>`・`<CLI>`）と既定のレジストリを引数で渡し、偽の HTTP サーバー（`httptest`）と一時
  ディレクトリで確かめます（環境変数は増やさない）。押さえること:
  - レジストリの順と規則の選び方（先に見つかった名前・最初に合った規則・取れなければ次へ進まない・
    `{tag}` のある規則でのタグの照合・知らない版と項目）
  - 新しいかの判定（同じタグとターゲットで何も変えない・`--check` で何も変えない・`--reinstall`）
  - sha256 が合わない・zip の形が正しくない（モジュールが無い・2 つある・シンボリックリンク・`..`・
    `\`）ときに何も変えない
  - 入れる先のフォルダ（記録が無い・`registry` が違うときに断る）
  - ロックが掴まれているときに何も変えない
  - 付け替えの途中の失敗と、フックの失敗で元に戻る
  - フックの走らせ方（引数・作業ディレクトリ・環境変数・標準出力が JSON でないときの失敗・
    `uninstall` で記録のフックを走らせる）。偽のフックを入れた zip を偽の HTTP サーバーから配る
  - 消すコードの歯止め（記録が無い・名前が違う・モジュールが無いフォルダを消さない）
  - 既定のレジストリ（`cli/internal/registry/default.json`）が正しく読めること
- **コマンドのテストでの場所の差し替え**: コマンド（`cli/cmd/vw2026`）は、上の関数に渡す場所を
  `env` から得ます。いまの `vw2026` のテストはスプールの場所を `env.spool` で差し替えます。段 3 で
  次のように改め、`CLAUDE.md`「Go」の `env.spool` の記述も同じ PR で直します。
  - `spool.DefaultDir()`（スプールを返す）を `spool.CLIDir()`（`<CLI>` を返す）に改める（名前を
    変え、`DefaultDir()` は残さない）。スプールは `filepath.Join(cli, spool.SpoolDirName)` で
    組み立てる。
  - `env.spool` を `env.cliDir`（`<CLI>` を返す）に改め、既定の `<Plug-Ins>` を返す
    `env.pluginsDir` を足す。本番では `main()` だけがそれぞれ `spool.CLIDir` と既定の
    `<Plug-Ins>` を求める関数（求め方は[名前と識別子「置き場所」](identifiers.md#置き場所)）を
    入れる。
  - テストの `invoke` は、どちらも既定で一時ディレクトリを返す関数を入れる。テストが場所を
    渡し忘れても、開発者のマシンの本物の `<Plug-Ins>`・`<CLI>` に触れないためです。
    `--plugins-dir` は、既定以外の場所を渡したときの振る舞いを確かめるテストだけが付けます。
  - `install` / `uninstall` は `env.cliDir` から `<CLI>` を得て、ロックは `<CLI>/spool` で見る。
    スプールと `<CLI>` を別々に差し替えられると、テストで両者が食い違いうるためです。
- **`cli` のフック**: CI の `hooks.yml`（[ビルド「CI」](build-and-release.md#ci)）で確かめます。どちらも
  一時ディレクトリを `HOME`・`VW2026_HOOK_*` に渡し、利用者の場所に触れません。
  - mac（`sh`）: `tests/hooks/hooks_test.sh`（`sh` で書いた小さなテスト。外部の枠組みを使わない）。
    `lib.sh` の関数と、`install.sh` / `uninstall.sh` の全体（置かれたもの・リンク・別の場所を指す
    リンクと利用者のファイルに触れない・出力・2 回走らせても同じ結果）を確かめる。
  - Windows（`powershell`）: `tests/hooks/Hooks.Tests.ps1`（Pester。版は `scripts/powershell.ps1` で
    固定し、Windows に入っている Pester 3.4 は使わない）。`lib.ps1` の `Path` の文字列の関数（足し方・
    除き方・比べ方。末尾の `;`・空になったとき・前後の空白と `"` を含む項目）と、`install.ps1` /
    `uninstall.ps1` の全体（レジストリの読み書きと知らせは差し替え、`<CLI>` は一時ディレクトリ）を
    確かめる。Windows PowerShell 5.1 で走らせる（フックが走るのと同じもの）。
- **実物の exe の入れ替え**: PR の `build.yml` で、`release`（PR のプレリリースを出す）の
  あとに Windows のジョブで確かめます。`install` は版の違うものしか入れ替えないので、版の違う
  2 つの公開済みのリリース（その PR のタグと `latest`）を使います。
  1. その PR のプレリリースの zip から `bin\vw2026.exe` を一時フォルダへ取り出し、
     `install cli@<その PR のタグ>` を走らせる（初回のスクリプトと同じ入り方）。
  2. 入った `<CLI>\bin\vw2026.exe` で `install cli@latest` を走らせる。動いているのは PR の
     ビルドで、`latest` のフックがそれを `latest` の CLI に入れ替える。
  3. 手順 2 の出力が `installed` で、`<CLI>\bin\vw2026.exe version` の版が `latest` の
     マニフェストの `tag` の版と一致し、`vw2026.exe.old-*` が残っていることを確かめる（比べる
     タグは改めて取らず、手順 2 の出力の `tag` を使う）。
  - 入れ替えを行うのは `latest` の側のフックです。`latest` にフックが無い間（段 3 を入れる PR
    自身）は、手順 2 は段 3 の前の形の `latest` を入れることになり組めないので、手順 1 と下の
    `Path` の確かめだけを行います。
  - `main` への push では、版の違う相手が無いので走らせません（PR で確かめてからマージする）。
  - ネットワークと公開済みのリリースに頼るのは、同じ位置にある `verify` ジョブと同じです。
  - スプールを使わないので、場所を変える指定もテストのための上書きも要りません（別の `vw2026`
    を動かしたまま入れ替える形は、ロックが掴まれていると `install` が何もせずに終わり、放され
    ると `call` も終わるので組めない）。ランナーは使い捨てなので既定の場所（`<Plug-Ins>`・
    `<CLI>`）を使いますが、利用者の場所に触れるので `go test` には入れません。
- **実物の exe の入れ替えのジョブ**（`build.yml` の `install-windows`）の条件と、ほかに確かめること:
  - `needs: [meta, release]` で、`release` が公開したあとにだけ走ります。`release` が飛ばされる
    fork の PR・マージキュー・`main` 以外の手動の実行では、ジョブも飛ばされます。`main` への push
    は `if` で除きます。
  - タグは使い回さず、リリースは下書きで資産を上げ終えてから公開するので（[ビルド「リリースの形」](build-and-release.md#リリースの形)）、
    資産が一瞬無いことはありません。ジョブは走らせ直しを持ちません。
  - **実物のレジストリで、`Path` の 3 つの型の扱い**（[Windows の `Path` の読み書き](#windows-の-path-の読み書き)）。
    手順 1 と 2 の間で、利用者の `Path` を置き直して `install cli@<その PR のタグ> --reinstall` を
    走らせ直し、次を確かめます（ランナーは使い捨てなので、元の値は戻さない）。
    1. 手順 1 の前に値を消しておく → 手順 1 のあと、REG_EXPAND_SZ で `<CLI>\bin` だけの値ができる。
    2. REG_SZ の `C:\vw2026-probe-sz` に置き直して走らせる → REG_SZ のまま
       `C:\vw2026-probe-sz;<CLI>\bin` になる。
    3. REG_EXPAND_SZ の `%USERPROFILE%\vw2026-path-probe;` に置き直して走らせる → REG_EXPAND_SZ の
       まま、項目が展開されずに残り、`%USERPROFILE%\vw2026-path-probe;<CLI>\bin` になる（末尾の
       `;` に区切りを重ねない）。
    4. 手順 2 のあとも同じ値のまま（`exists`）。
- **初回のスクリプト**: shellcheck・PSScriptAnalyzer。取ってきて `install` を走らせるだけなので、
  実機の確認（段 3）で押さえます。
