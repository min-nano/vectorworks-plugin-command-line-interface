# 初回の配置・インストール・更新・アンインストール

**利用者はまず CLI を入れ、CLI がプラグインを置きます。** インストーラとアンインストーラの
スクリプトは持ちません。

| 段 | 手段 | すること |
| --- | --- | --- |
| 初回の配置 | `get-vw2026.sh` / `.ps1`（curl などで取ってパイプで実行） | CLI を `<CLI>/bin/` に置き、PATH を通す。最後に `vw2026 install` を案内する |
| インストール・更新 | `vw2026 install` | プラグインを `<Plug-Ins>/cli/` に置く（入っている版と同じなら何もしない）。CLI 自身も同じビルドに入れ替える |
| アンインストール | `vw2026 uninstall` | プラグイン・CLI・スプール・PATH の項目を取り除く |

```sh
# macOS
curl -fsSL https://github.com/min-nano/vectorworks-plugin-command-line-interface/releases/download/stable/get-vw2026.sh | sh
vw2026 install
```

```powershell
# Windows
irm https://github.com/min-nano/vectorworks-plugin-command-line-interface/releases/download/stable/get-vw2026.ps1 | iex
vw2026 install
```

## 前提

- **プラグインのフォルダ（`<Plug-Ins>`）と CLI の場所（`<CLI>`）は同じドライブ（ボリューム）に
  あるものとします。** 既定ではどちらも利用者のホームの下です。入れ替えは名前の変更だけで
  行い、ボリュームをまたぐ場合の扱い（コピーへの切り替え・その検出）は持ちません。名前の
  変更が失敗したら、何も変えずに失敗で終わります。
- **入れた場所を記録しません。** 既定以外の `<Plug-Ins>` を使う利用者は、`install` と
  `uninstall` に毎回 `--plugins-dir` を渡します（[設計「CLI はプリミティブに保つ」](../design.md#cli-はプリミティブに保つ)）。
- **安定版と開発版を区別しません。** PR のプレリリースを入れると、プラグインも CLI も
  そのビルドのものに入れ替わります。
- **隔離属性の解除（mac）・`Unblock-File`（Windows）は行いません。** curl・`Invoke-RestMethod`・
  Go の HTTP で取ったファイルには、隔離属性も Web からの印（Mark of the Web）も付かないためです。
  署名は元と同じくビルドがかけたアドホック署名で、zip から取り出したファイルにそのまま残ります。

場所は[名前と識別子「置き場所」](identifiers.md#置き場所)にあります。

## 初回の配置（`get-vw2026.sh` / `.ps1`）

**CLI を既定の場所へ置き、PATH を通すだけ**のスクリプトです。プラグインには触れません
（したがって Vectorworks が動いていても走らせてよい）。引数を持ちません。

1. `stable` の zip（mac は `cli.vwlibrary.zip`、Windows は `cli.vlb.zip`）を一時フォルダへ
   取ってきて、中の `bin/vw2026`（Windows は `bin\vw2026.exe`）だけを取り出す。
2. `<CLI>/bin/` の中へ一時的な名前で置き、名前の変更で入れ替える（下記「CLI の入れ替え」と
   同じ手順。走らせ直しても壊れない）。
3. PATH を通す（下記「PATH」）。
4. `vw2026 install` を走らせるよう表示して終わる。新しい端末で効くことも添える（Windows）。

- CLI だけの資産は持ちません。zip から取り出すので、CLI と同じビルドのものしか配らずに済みます。
- PR のプレリリースを試すときも、まず `stable` で CLI を置き、`vw2026 install --tag dev-<slug>`
  で入れ替えます（`install` は CLI も入れ替える）。
- 置き場所は `vw2026` と同じ規則で決めます（mac `~/Library/Application Support/vectorworks2026-cli/`、
  Windows `%LOCALAPPDATA%\vectorworks2026-cli\`）。

### PATH

| OS | すること | 既にあるとき |
| --- | --- | --- |
| mac | `~/.local/bin/vw2026` → `<CLI>/bin/vw2026` のシンボリックリンク（`~/.local/bin` が無ければ作る） | **このプラグインの CLI を指すリンクなら張り直す。** それ以外（利用者のファイル・別の場所を指すリンク）なら触らず、その旨を表示する |
| Windows | 利用者の環境変数 `Path`（`HKCU\Environment`）に `<CLI>\bin` を追加 | 同じ項目が既にあれば何もしない |

- 行き先がプラグインのフォルダの外で版によらず同じなので、**更新でリンクと PATH を張り直す
  必要はありません**（`install` は PATH に触れない）。
- **シェルの設定ファイル（`.zshrc` 等）は書き換えません。** `~/.local/bin` が PATH に無ければ、
  追加の方法を表示するだけです。
- Windows は `[Environment]::SetEnvironmentVariable('Path', …, 'User')` で書きます（`setx` は
  1024 文字で切り詰めるので使わない）。新しい値は**新しく開いた端末から**効きます。

## インストールと更新（`vw2026 install`）

**初回のインストールも更新も、同じ `install` で行います。**

```sh
vw2026 install                            # main の最新を既定の <Plug-Ins> へ（同じ版なら何もしない）
vw2026 install --plugins-dir <Plug-Ins>   # 既定以外の <Plug-Ins> へ
vw2026 install --tag dev-feature-x        # その PR のプレリリースの最新へ（同じ名前で入れ替わる）
vw2026 install --check                    # 入れずに、新しいビルドがあるかだけ返す（動いていてもよい）
```

### 流れ

```
vw2026 install
  1. <Plug-Ins> を決める              --plugins-dir、無ければ既定
  2. zip を取ってきて展開する         https://github.com/<repo>/releases/download/<tag>/<zip>
                                     （<tag> は stable、--tag があればそれ）→ <CLI>/staging/cli/
  3. 版を比べる                       zip の中の build.json と <Plug-Ins>/cli/build.json の version
                                     同じなら終わり {"outcome":"up_to_date"}
                                     --check なら入れずに {"outcome":"available"}
  4. Vectorworks が動いていないか     ロックが掴まれていれば終わり {"outcome":"vectorworks_running"}（終了コード 7）
  5. プラグインのフォルダを付け替える  下記「入れ替えの手順」
  6. CLI を入れ替える                 下記「CLI の入れ替え」
  7. 後始末                           <CLI>/staging/・<CLI>/old/ を消す
                                     {"outcome":"installed","version":…,"plugins_dir":…}
```

- **GitHub の API を使いません。** リリースの資産は、決まったタグと資産名から URL が決まるので、
  直接取ってきます。API の回数制限・トークンの扱い・リリース本文の読み取りが要りません。
  zip は小さいので、新しいかどうかも取ってきてから中身で判定します。
- **プラグインと CLI は 1 つの zip から取ります。** `stable` が取り直しの間に作り直されても、
  入る 2 つは必ず同じビルドです（[作法「版」](../protocol.md#版)の前提）。
- **配置の規則: zip の直下の `bin/` は CLI の場所（`<CLI>/bin/`）へ、それ以外はそのまま
  `<Plug-Ins>/cli/` へ置く。** ファイル名を列挙しません。展開では権限（実行ビット）を保ちます。
  シンボリックリンクを含む zip は失敗として扱います（ビルドが作らない）。
- **新しいかは `build.json` の `version`（短い sha）だけで比べます**（sha が同じならコードも同じ。
  `branch` は表示のため）。入っている `build.json` が読めないときは「入っていない」とみなして入れます。
- **Vectorworks が動いているかはロックで判定します**（[作法「生存の判定」](../protocol.md#生存の判定)）。
  プロセス名は見ません。ロックを掴んでいない Vectorworks（プラグインが入っていない・起動直後の
  10 秒・2 つ目の起動）が居るときは、Windows では読み込み中のフォルダの名前を変えられないので
  付け替えが失敗し（終了コード 6）、何も変わりません。mac では付け替えが成功し、その
  Vectorworks は古い版のまま動き、次の起動から新しい版になります。
- **終了させるのは呼ぶ側です。** `install` は終了を頼まず、待ちもしません（[設計「CLI は
  プリミティブに保つ」](../design.md#cli-はプリミティブに保つ)）。終了から起動し直すまでを 1 度に
  行いたい呼ぶ側は、次のように組み合わせます（`quit` は保存の確認を出す。[道具](tools.md#quit)。
  この組み合わせの真実はここで、ほかのページはここを参照します）。

  ```sh
  vw2026 call quit && vw2026 wait --down && vw2026 install && vw2026 launch && vw2026 wait
  ```

- `--tag` はタグをそのまま受け取ります（ブランチ名から slug を作らない。slug の規則は
  [名前と識別子](identifiers.md)）。
- 失敗したら（取れない・展開できない・付け替えられない）、何も変えずに理由を `message` に載せて
  終了コード 6 で終わります。

### 入れ替えの手順

1. **組み立て**: zip を `<CLI>/staging/cli/` に展開し、`bin/` を `<CLI>/staging/bin/` へ移す
   （残りがプラグインのフォルダの中身）。前回の残りがあれば先に消す。
2. **付け替え**: `<Plug-Ins>/cli` → `<CLI>/old/cli`、続けて `<CLI>/staging/cli` →
   `<Plug-Ins>/cli`。1 つ目が失敗したら何もせずに終わる（エクスプローラーやウイルス対策が
   フォルダの中のファイルを開いているとき・Vectorworks が読み込んでいるとき）。2 つ目が
   失敗したら 1 つ目を戻す。まだ入っていなければ 1 つ目は飛ばす。

- 付け替えを名前の変更で行うのは、途中で失敗したときに元へ戻せるようにするためです（ファイル
  ごとに置き換えると、半端な版が残る）。
- 組み立てと退避の場所を `<CLI>` に置くのは、`Plug-Ins` の外（Vectorworks に読まれない）で、
  前提により `<Plug-Ins>` と同じドライブにあるためです。

### CLI の入れ替え

**プラグインを入れ終えてから**行います（プラグインの入れ替えが失敗したときに、CLI だけが
新しくならないように）。入れ替える CLI は `install` を走らせている本人で、ほかの呼び出しとしても
動いていることがあります。動いている実行ファイルは、mac では上書きでき（動いているプロセスは
古い inode を使い続ける）、Windows では上書きも削除もできませんが、**名前の変更はできます**。

1. `<CLI>/staging/bin/` の新しい CLI を `<CLI>/bin/` の中へ一時的な名前（`vw2026.new` /
   `vw2026.exe.new`）で移す。
2. mac: `vw2026.new` を `vw2026` へ名前を変える（上書き）。
   Windows: `vw2026.exe` を `vw2026.exe.old-<時刻>` へ名前を変え、`vw2026.exe.new` を
   `vw2026.exe` へ名前を変える。2 つ目が失敗したら 1 つ目を戻す。
3. 残っている `vw2026.exe.old-*` を消してみる（動いている間は消せないので、失敗は無視して
   次の `install` で消す）。

- 名前を変えられた古い CLI は、動いている間はそのまま動き続けます。次に呼ばれたときから
  新しい CLI です。
- 初回の配置のスクリプトも同じ手順で CLI を置きます。

## アンインストール（`vw2026 uninstall`）

```sh
vw2026 uninstall                          # 既定の <Plug-Ins> から
vw2026 uninstall --plugins-dir <Plug-Ins> # 既定以外の <Plug-Ins> から
```

消すもの: `<Plug-Ins>/cli/`・スプール（`<CLI>/spool/`）・組み立てと退避の場所・CLI
（`<CLI>/bin/`）・リンクと PATH の項目。最後に `<CLI>` が空なら `<CLI>` も消します。

- **Vectorworks が動いていれば何もせずに終わる**（ロックで判定。終了コード 7）。
- **安全弁**: プラグインのフォルダは、フォルダ名が `cli` と一致し、中にモジュール
  （`cli.vwlibrary` / `cli.vlb`）があるときだけ消す。無ければ消さずに成功として扱う。
  `<CLI>` の下は、決まった名前（`bin` / `spool` / `staging` / `old`）のものだけを消す。
- 動いている CLI（`uninstall` 自身）は、mac では消せます。Windows では消せないので
  `vw2026.exe.old-<時刻>` へ名前を変えて残し、結果に `"left":[…]` として載せます
  （アンインストールは失敗にしない）。
- **PATH の後始末**:
  - mac: `~/.local/bin/vw2026` が**シンボリックリンクで、`<CLI>/bin/` の中を指すときだけ**消す。
  - Windows: 利用者の `Path` から、`<CLI>\bin` と**完全に一致する項目だけ**を除く（Go から
    PowerShell の `[Environment]::SetEnvironmentVariable` を呼ぶ。外部の依存を足さないため）。
- 出力: `{"outcome":"uninstalled","left":[…]}`。

## 消すコードの歯止め

**利用者のものを消すコードは次の場所だけ**にし、歯止めを緩めません（元のプラグインの規約に
倣う）。段 5 でこの表を `CLAUDE.md` に写します。

| 消すコード | 消してよいもの |
| --- | --- |
| `install`（組み立て・後始末） | `<CLI>/staging/`・`<CLI>/old/`（退避した版は、名前が `cli` でモジュールがあるときだけ） |
| `install`・初回の配置（CLI の入れ替え） | `<CLI>/bin/` の中の `vw2026.exe.old-*`・`vw2026.new` / `vw2026.exe.new` |
| `uninstall` | 上に挙げたフォルダ・リンク・PATH の項目 |

## テスト

- **`install` / `uninstall`**: Go の単体テスト。場所（`<Plug-Ins>`・`<CLI>`）と資産の URL は
  関数の引数で渡し、偽の HTTP サーバー（`httptest`）と一時ディレクトリで確かめます
  （環境変数は増やさない）。押さえること:
  - 新しいビルドの判定（同じ版で何も変えない・`--check` で何も変えない）
  - ロックが掴まれているときに何も変えない
  - 付け替えの途中の失敗で元に戻る
  - 消すコードの歯止め（名前が違う・モジュールが無いフォルダを消さない、別の場所を指すリンクと
    似た名前の PATH 項目を消さない）
  - **動いている exe の名前を変えられること**（Windows の CI。`vw2026 wait` を動かしたまま入れ替える）
- **初回の配置のスクリプト**: shellcheck・PSScriptAnalyzer。中身は短く、CLI を置くことと
  PATH だけなので、実機の確認（段 4）で押さえます。
