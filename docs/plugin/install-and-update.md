# インストール・PATH・アンインストール・自動アップデート

元:`scripts/vw-install.*` / `vw-uninstall.*` / `vw-update.*` / `vw-token.*` と元:`src/Updater*` を移し、
**PATH の扱いだけを足します**。仕組みの説明は元:`docs/development/auto-update/`。

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

## 自動アップデート

- 入口は **メニュー「アップデートを確認」** と **道具 `update`** の 2 つで、どちらも
  `src/UpdaterFlow.cpp` の 1 本の経路を通ります（2 本目を書かない）。
- メニューはダイアログで尋ね、`update` は尋ねません（元:`RemoteDevUpdateWith` を安定版にも
  使えるよう一般化する）。
- 同梱スクリプト（`vw-update.*`）は**問い合わせとダウンロードだけ**で、配置は zip の中の
  インストーラに任せます（走るのは常にインストール済みの古い版なので）。
- `VW_REPO` の既定は `min-nano/vectorworks-plugin-command-line-interface`。プラグイン名
  （`cli` / `cli_dev`）の固定値を置き換えます。
- 殻の ID が変わらなければ本体を降ろして読み直すだけ（再起動なし）。変わったら再起動を
  勧めます。判定できなければ「再起動が要る」とします（元:`NeedsRestartAfterInstall`）。
- **CLI（`bin/vw2026`）は本体と同じく、置き換えれば次に呼んだときから新しい版です**
  （殻の ID に入れない）。

### トークン（`vw-token.*`）

GitHub の API は認証なしだと IP ごとに 1 時間 60 回までなので、あれば使います（無くても動く）。
名前を元から変えます: 環境変数 `VW_CLI_GITHUB_TOKEN`・Keychain のサービス `VectorworksCliGitHub`・
Windows の `%LOCALAPPDATA%\VectorworksCli\github-token.dat`。順に探し、最後に `gh auth token`。
