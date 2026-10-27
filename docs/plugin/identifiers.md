# 名前と識別子

名前と置き場所の真実はこのページです（ほかのページはここを参照する）。
**スプールの場所・コマンド名・リンクの名前・インストール先は、`vw2026 install` で配る最初の版（段 3）の
あとは変えません**（変えると呼ぶ側や更新が見つけられなくなる）。段 2 は利用者が手で置いて確かめるための
版で、段 3 で入れ直す（[実装の順序](plan.md)の段 3）ので、段 3 でスプールの場所と CLI の置き場所を改めます。

**安定版と開発版を区別しません。** PR では変更後のコードでビルドしたものをプレリリースし、
同じ名前のプラグインとして入れ替えます（`vw2026 install cli@<そのタグ>`。[ビルド「リリースの形」](build-and-release.md#リリースの形)）。
いま何が動いているかは、`vw2026 call ping` の `version` / `branch` で分かります。

| 項目 | 値 |
| --- | --- |
| プラグイン名（ファイル名・フォルダ名・`PLUGIN_VWR_ID`） | `cli` |
| モジュール | `cli.vwlibrary` / `cli.vlb` |
| リソース | `cli.vwr` |
| バンドル ID（mac） | `io.github.min-nano.cli` |
| コマンド | `vw2026` |
| レジストリの名前（プラグインのフォルダ名） | `cli`（既定のレジストリの名前。[レジストリ](../registry.md)） |
| CMake のターゲット | `VwCli` |
| リリースのタグ | **ビルドごとに新しいタグを付け、使い回さない**（[レジストリ「名前・タグ・ターゲット」](../registry.md#名前タグターゲット)）。main は `0.0.0-<短い sha>`（`latest` のリリース。形がある程度決まったら sha を外し、`0.0.1` から順にセマンティック バージョニングに倣って上げる。そのときにこの表を改める）、PR は `dev-<ブランチの slug>-<PR 番号>-<短い sha>`（プレリリース）。短い sha はビルドした sha（PR はブランチの head）。slug はブランチ名の `/`・`:`・`@`・空白を `-` にし、英数字・`-`・`_`・`.` のほかを落とし、続く `.` を 1 つにまとめ、**先頭から 40 文字で切り、末尾の `-`・`_`・`.` を落としたもの**（`..` は git の ref 名に使えない。切るのは、タグを 128 文字の上限（[レジストリ「名前・タグ・ターゲット」](../registry.md#名前タグターゲット)）に収めるため）。slug は読むための飾りで、別のブランチが同じ slug になりうるので一意にするのは PR 番号と sha。slug が空（日本語だけの名前など）なら `dev-<PR 番号>-<短い sha>`。作るのは `scripts/dev-tag.sh` だけで、`build.yml` と `cleanup-dev-release.yml` が使う。最新は `latest`（GitHub のリリースの最新。`cli@latest`）で指す |
| ターゲット | `macos-arm64`・`windows-x64`（このプラグインが配るもの。`macos-x64` は配らない。[ビルド「CMake」](build-and-release.md#cmake)） |

## 置き場所

| 置くもの | mac | Windows |
| --- | --- | --- |
| プラグイン（`<Plug-Ins>/<name>/`。このプラグインは `<Plug-Ins>/cli/`） | `~/Library/Application Support/Vectorworks/2026/Plug-Ins/cli/` | `%APPDATA%\Nemetschek\Vectorworks\2026\Plug-Ins\cli\` |
| 入れたときの記録 | `<Plug-Ins>/<name>/vw2026-install.json` | `<Plug-Ins>\<name>\vw2026-install.json` |
| CLI（PATH に載せる） | `<Plug-Ins>/cli/bin/vw2026` | `<Plug-Ins>\cli\bin\vw2026.exe` |
| スプール（プラグインが作る） | `<Plug-Ins>/cli/spool/` | `<Plug-Ins>\cli\spool\` |
| `vw2026` の作業の場所（`<Work>`。[インストール「作業の場所」](install-and-update.md#作業の場所)） | `/var/folders/…/T/vectorworks2026-cli/` | `%LOCALAPPDATA%\Temp\vectorworks2026-cli\` |
| 組み立ての場所 | `<Work>/staging/<name>/`（zip は `<Work>/staging/<name>.zip`） | `<Work>\staging\<name>\` |
| 退避の場所 | `<Work>/old/<name>/` | `<Work>\old\<name>\` |
| `uninstall` の途中の印 | `<Work>/old/<name>.uninstalling` | `<Work>\old\<name>.uninstalling` |
| よけた CLI（[インストール「動いている CLI のよけ方」](install-and-update.md#動いている-cli-のよけ方)） | `<Work>/old/vw2026.old-<時刻>` | `<Work>\old\vw2026.exe.old-<時刻>` |
| PATH | `~/.local/bin/vw2026`（`<Plug-Ins>/cli/bin/vw2026` へのリンク） | 利用者の `Path` に `<Plug-Ins>\cli\bin` |

上の表は段 3 からのものです。段 2 までの実装（いまのコード）は、スプールを CLI の場所
（mac は `~/Library/Application Support/vectorworks2026-cli/`、Windows は `%LOCALAPPDATA%\vectorworks2026-cli\`。
[通信規則「スプールの場所」](../protocol.md#スプールの場所)の `<CLI>`）の下の `spool/` に置き、CLI を置く場所を決めていません。
段 3 からは、持続する `vw2026` の場所を持ちません。

- **場所の根は環境変数を読まずに求めます。** スプールは、プラグインは自分のモジュールの場所から、
  `vw2026` は自分の実行ファイルの場所から求めます（[通信規則「スプールの場所」](../protocol.md#スプールの場所)）。
  `<Work>` と既定の `<Plug-Ins>` は OS の決まった場所から求め、GUI アプリの Vectorworks と、ssh や別のアプリ
  から起動した `vw2026` とで食い違わないようにします。
  - `<Work>`: Windows は `SHGetKnownFolderPath(FOLDERID_LocalAppData)` の下の `Temp`（環境変数 `TMP` /
    `TEMP` は読まない。`vw2026` は `syscall` 経由）。mac は `/usr/bin/getconf DARWIN_USER_TEMP_DIR` の出力
    （環境変数 `TMPDIR` は読まない）。その下の `vectorworks2026-cli`（[インストール「作業の場所」](install-and-update.md#作業の場所)）。
  - 既定の `<Plug-Ins>`: Windows は `%APPDATA%` の指す場所を `SHGetKnownFolderPath(FOLDERID_RoamingAppData)` で求める
    （環境変数 `APPDATA` は読まない。`syscall` 経由）。mac はホームディレクトリの `Library/Application Support` の下で、
    ホームは利用者の passwd の項目（`getpwuid_r(getuid())` の `pw_dir`）から求める（環境変数 `HOME` は読まない。
    `vw2026` は `os/user`）。
- `<Plug-Ins>` は Vectorworks の利用者フォルダの中です。Vectorworks の設定で利用者フォルダを
  移せるので、標準以外の場所も、`<Work>` と同じドライブにあれば扱えます（`install` / `uninstall`
  に毎回 `--plugins-dir` で渡す。入れた場所は記録しない。[インストール](install-and-update.md#前提)）。
  ただし `cli` は利用者ごとの `Plug-Ins` にだけ置きます（ワークグループのフォルダなど、複数の利用者が読み込む
  場所に置くとスプールも共有される。[設計「決めたこと」](../design.md#決めたこと)）。
- `<Work>` は、このプラグインだけでなく、`vw2026` で入れるすべてのプラグインの組み立て・退避の場所です
  （`<name>` ごとに分ける）。CLI とスプールは置きません（`cli` のフォルダの中にある。
  [インストール「CLI とスプールの置き場所」](install-and-update.md#cli-とスプールの置き場所)）。
  PATH はこのプラグインのフックが置きます（[インストール「`cli` のフック」](install-and-update.md#cli-のフック)）。

## 拡張機能

**登録しません**（[構成](architecture.md#拡張機能を登録しない)）。拡張機能なしで読み込まれない
ことが実機で分かったときだけ、メニューコマンドを 1 つ登録し、そのユニバーサル名と UUID を
ここに足します。

## `src/BuildConfig.h`

```cpp
#define PLUGIN_VWR_ID   "cli"
// VW_BUILD_VERSION / VW_BUILD_BRANCH は CMake が構成のたびに書く BuildStamp.h にある
// （-D で渡さなければ "local"）
#include "BuildStamp.h"
```

`<CLI>` のディレクトリ名（`vectorworks2026-cli`）とスプールの名前（`spool`）は、スプールの場所を
求める共通部（SDK に依らない）が使うので、`src/core/Protocol.h` の `kAppDirName` / `kSpoolDirName`
に置きます。CLI 側の `cli/internal/spool` の `AppDirName` / `SpoolDirName` と対で、両側のテストが
[通信規則の見本](../fixtures.md)の `constants.json` に照らします（[通信規則](../protocol.md#スプールの場所)）。
段 3 でスプールを `<Plug-Ins>/cli/spool/` に移すと、プラグインは `<CLI>` を使わなくなるので、
`kAppDirName` と見本の `app_dir` を除き、`AppDirName` は `vw2026`（`<Work>` のディレクトリ名）だけが持ちます。
CLI の実行ファイルを置く `bin` は `vw2026` がスプールを求めるのに使うので、`cli/internal/spool` に置きます
（プラグインは使わないので、見本には載せない）。
