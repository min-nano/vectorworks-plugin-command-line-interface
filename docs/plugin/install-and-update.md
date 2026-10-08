# インストール・PATH・アンインストール・更新

インストーラとアンインストーラは元:`scripts/vw-install.*` / `vw-uninstall.*` を移し、
**PATH の扱いだけを足します**。**更新は CLI（`vw2026 update`）が行い**、元の殻の中の
アップデータ（元:`src/Updater*`）と同梱スクリプト（元:`vw-update.*` / `vw-token.*`）は移しません。

## インストーラ（`vw-install.sh` / `.ps1`）

- **配置の規則はひとつ: zip の直下にあるものを、そのまま `<Plug-Ins>/<name>/` へ置く。**
  ファイル名を列挙しない（`bin/` もこの規則で入る）。除くのはインストーラ自身と
  `__MACOSX` / `.DS_Store` だけ。
- 入れる前に、いま入っている版をその版自身のアンインストーラで取り除く。
- mac: 置いたものの隔離属性を外し（`xattr -dr com.apple.quarantine`）、アドホック署名をかけ直す
  （`bin/vw2026` も対象）。Windows: `Unblock-File`。読み込み中の `.vlb` は消さずに名前を変える。
- 既定の `<Plug-Ins>`: mac `~/Library/Application Support/Vectorworks/2026/Plug-Ins`、Windows
  `%APPDATA%\Nemetschek\Vectorworks\2026\Plug-Ins`。
- 機械可読の出力: `installed-shell=<VW_SHELL_ID>` → `ok` / `error=`。

### PATH（追加分）

置き終えてから、**安定版のときだけ**行います（開発版はリンクを張らない。`vw2026 --channel dev`
で開発版の橋へ届くので、コマンドは 1 つでよい）。

| OS | すること | 既にあるとき |
| --- | --- | --- |
| mac | `~/.local/bin/vw2026` → `<Plug-Ins>/cli/bin/vw2026` のシンボリックリンク（`~/.local/bin` が無ければ作る） | **このプラグインを指すリンクなら張り直す。** それ以外（利用者のファイル・別の場所を指すリンク）なら触らず、`path=conflict` と出す |
| Windows | 利用者の環境変数 `Path`（`HKCU\Environment`）に `<Plug-Ins>\cli\bin` を追加 | 同じ項目が既にあれば何もしない |

- **シェルの設定ファイル（`.zshrc` 等）は書き換えません。** `~/.local/bin` が PATH に無ければ、
  追加の方法を表示するだけです（`path=not-in-path`）。
- Windows は `[Environment]::SetEnvironmentVariable('Path', …, 'User')` で書きます（`setx` は
  1024 文字で切り詰めるので使わない）。新しい値は**新しく開いた端末から**効きます。
- 機械可読の出力に `path=linked|added|exists|not-in-path|conflict` を加えます。PATH の失敗で
  インストールを失敗にはしません（プラグインは動く）。

## アンインストーラ（`vw-uninstall.sh` / `.ps1`）

- `<Plug-Ins>/<name>/` を丸ごと消す。**安全弁**: フォルダ名が `<name>` と一致し、中に殻
  （`<name>.vwlibrary` / `<name>.vlb`）があるときだけ。無ければ成功として扱う。
- **PATH の後始末（追加分）**:
  - mac: `~/.local/bin/vw2026` が**シンボリックリンクで、消すフォルダの中を指すときだけ**消す。
  - Windows: 利用者の `Path` から、消すフォルダの `bin` と**完全に一致する項目だけ**を除く。
- 更新では「アンインストール → インストール」の順に走るので、リンクと PATH はいったん消えて
  張り直されます（行き先は同じ）。

**利用者のものを消すコードが増えます。** 元のプラグインの規約（消すコードは数か所だけで、
歯止めを緩めない）に倣い、このリポジトリの `CLAUDE.md` に「消すのはアンインストーラの
フォルダ・リンク・PATH の項目だけ」と書き、上の歯止めを回帰テストで押さえます
（元:`tests/vw-uninstall.test.sh` / `.Tests.ps1` に、別の場所を指すリンク・利用者のファイル・
似た名前の PATH 項目を消さないことを足す）。

## 更新（`vw2026 update`）

**更新は CLI のコマンドで行います。** プラグインにメニューもアップデータも持たせません。

```sh
vw2026 update                      # 安定版の最新を入れる
vw2026 update --channel dev --branch feature-x   # 開発版のそのブランチの最新を入れる
vw2026 update --check              # 入れずに、新しいビルドがあるかと再起動が要るかだけ返す
vw2026 update --restart            # 再起動が要る更新なら、Vectorworks を終了させて入れ替え、起動し直す
```

### なぜ CLI なのか

- **本体が自分を置き換えられない問題が起きません。** 元のプラグインで更新が殻にあったのは、
  更新する処理が本体の中にあると、実行中の自分を置き換えることになるからでした。CLI は
  Vectorworks の外のプロセスなので、その制約がありません。
- **殻が小さくなります。** 殻にアップデータ・ダイアログ・同梱スクリプトの実行が要らなくなり、
  殻の ID が変わる（＝再起動が要る）変更が減ります。
- **本体が読み込めない状態からも直せます。** ブリッジが動いていなくても CLI は動くので、
  メニューを復旧の経路として残す必要がありません。

### 流れ

```
vw2026 update
  1. 入っているビルドを読む            <プラグインのフォルダ>/build.json
  2. 新しいビルドを探す               GitHub のリリース（stable / dev-<slug>）
  3. 同じなら終わり                    {"outcome":"no_new_build"}
  4. zip を取ってきて一時フォルダへ展開し、zip の build.json と比べる
  5. 振り分け
     ├─ Vectorworks が動いていない            → すぐ入れる（zip の中の vw-install）
     ├─ 動いていて、殻の ID が同じ            → すぐ入れる。本体はブリッジの次の受け付けで
     │                                          読み直される（再起動なし）。新しい version が
     │                                          生存の印に出るまで待って確かめる
     └─ 動いていて、殻の ID が違う            → 入れずに置いておき、終了を待つ係を起こす
                                                {"outcome":"scheduled","restart_required":true}
```

- **入れるのは常に zip の中のインストーラ**です（配置の知識は新しいビルドの側にある。元の
  M21・M22 の教訓）。CLI はダウンロード・展開・振り分けだけを持ちます。
- `build.json` はビルドが zip の直下に置くファイルで、配置の規則どおりプラグインのフォルダへ
  入ります。`{"plugin","channel","version","branch","shell_id"}`。CLI はこれで「何が入っているか」
  と「殻が変わるか」を判定します（mac の `Info.plist` を読まずに済む）。
  **読めないときは「殻が変わる」とみなします**（判定できないなら再起動が要る側に倒す。元の
  `NeedsRestartAfterInstall` と同じ）。
- プラグインのフォルダは、CLI 自身の場所（`<フォルダ>/bin/vw2026`。リンクを辿る）から求めます。
  開発版はその隣の `cli_dev`。`--plugins-dir` で明示もできます。
- Vectorworks が動いているかは、生存の印の `pid` で判定します。印が無いのに動いている
  （プラグインが読み込めていない等）こともあるので、プロセス名でも確かめます
  （mac `pgrep -x "Vectorworks 2026"` 相当、Windows `tasklist`）。

### 殻が変わるときは、終了を待って入れ替える

殻は起動時にしか読み込まれず、Windows は読み込み中の `.vlb` を置き換えられません。また、
殻より先に新しい本体だけが入ると、古い殻が新しい本体を読めず（ABI の版が違うと）ブリッジが
止まります。そこで**殻が変わる更新は、Vectorworks の終了を待ってから丸ごと入れます。**

1. 展開したビルドを**待機の場所**へ移す（mac `~/Library/Application Support/vectorworks2026-cli/pending/`、
   Windows `%LOCALAPPDATA%\vectorworks2026-cli\pending\`）。一時ディレクトリは OS に
   掃除されうるので使わない。
2. **終了を待つ係**を切り離して起こす: `vw2026 _apply-pending --wait-pid <pid>`
   （CLI 自身。端末を閉じても残る。mac は `setsid`、Windows は `DETACHED_PROCESS`）。
3. 係は Vectorworks のプロセスが終わるのを待ち（mac `kqueue` の `NOTE_EXIT`／`kill(pid,0)` の
   ポーリング、Windows `WaitForSingleObject`）、終わったら待機の場所の zip のインストーラを走らせ、
   結果を `pending/result.json` に残して自分を消す。
4. CLI は `{"outcome":"scheduled","restart_required":true,"message":"Vectorworks を終了すると入れ替わります"}`
   を返す。**再起動が要ることの案内はこれで行います**（ダイアログは出さない）。

- **待っている更新がある間に `vw2026 update` をもう一度呼ぶと**、待機の場所を新しいビルドで
  置き換え、係が居なければ起こし直します（計算機の再起動で係が消えた場合もここで戻る）。
- `vw2026 status` は、待っている更新（`pending`）と前回の結果（`last_update`）も返します。
- 係が入れ替えている最中に Vectorworks が起動し直されると、半端な状態を読みうるので、係は
  入れ替えの前にもう一度プロセスが無いことを確かめ、居れば次の終了を待ち直します。

### `--restart`

殻が変わる更新で `--restart` を付けると、待つ係を起こしたあとで道具 `quit`（保存の確認を出して
終了させる。[道具](tools.md#quit)）を呼び、**係が入れ替えてから Vectorworks を起動します**。

- Vectorworks 自身の再起動（`CloseAllFilesAndQuitVectorworks` の再起動の指定）は使いません。
  入れ替えより先に起動し直してしまうためです。
- 利用者が保存の確認で取り消したら Vectorworks は終わらないので、係は待ち続けます（次に
  終了したときに入れ替わる。起動はしない）。

### トークン

GitHub の API は認証なしだと IP ごとに 1 時間 60 回までなので、あれば使います（無くても動く）。
環境変数 `VW2026_GITHUB_TOKEN`、無ければ `gh auth token` の出力。キーチェーンは読みません
（CLI に外部の依存を足さないため。要るならラッパー側で環境変数に入れる）。

### 歯止め

- `update` が消すのは、インストーラとアンインストーラが消すもの（上記）と、自分の待機の場所だけです。
- 入れ替えは**常にインストーラの経路 1 本**です（CLI に配置を書かない）。
- テスト: 振り分け（殻の ID の比較・判定できないとき・動いているかの判定）は Go の単体テストで、
  待つ係はプロセスの終了を偽の子プロセスで確かめます。
