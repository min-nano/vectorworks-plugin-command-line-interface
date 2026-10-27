# 名前と識別子

名前と置き場所の真実はこのページです（ほかのページはここを参照する）。
**スプールの場所・コマンド名・リンクの名前・インストール先は、最初の配布のあとは変えません**
（変えると呼ぶ側や更新が見つけられなくなる）。

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
| CLI の場所（`<CLI>`） | `~/Library/Application Support/vectorworks2026-cli/` | `%LOCALAPPDATA%\vectorworks2026-cli\` |
| CLI（PATH に載せる） | `<CLI>/bin/vw2026` | `<CLI>\bin\vw2026.exe` |
| スプール（プラグインが作る） | `<CLI>/spool/` | `<CLI>\spool\` |
| 組み立ての場所 | `<CLI>/staging/<name>/`（zip は `<CLI>/staging/<name>.zip`） | `<CLI>\staging\<name>\` |
| 退避の場所 | `<CLI>/old/<name>/` | `<CLI>\old\<name>\` |
| フックが使ってよい場所 | `<CLI>/data/<name>/` | `<CLI>\data\<name>\` |
| PATH | `~/.local/bin/vw2026`（`<CLI>/bin/vw2026` へのリンク） | 利用者の `Path` に `<CLI>\bin` |

- **場所の根は環境変数を読まずに求めます**（`<CLI>` とスプールは[通信規則「スプールの場所」](../protocol.md#スプールの場所)の
  規則）。既定の `<Plug-Ins>` も同じ規則にそろえ、GUI アプリの Vectorworks と、ssh や別のアプリ
  から起動した `vw2026` とで食い違わないようにします。
  - Windows: `%APPDATA%` の指す場所を `SHGetKnownFolderPath(FOLDERID_RoamingAppData)` で求める
    （環境変数 `APPDATA` は読まない。`<CLI>` の `FOLDERID_LocalAppData` と同じく `syscall` 経由）。
  - mac: `<CLI>` と同じく、passwd の項目のホームの下の `Library/Application Support`。
- `<Plug-Ins>` は Vectorworks の利用者フォルダの中です。Vectorworks の設定で利用者フォルダを
  移せるので、標準以外の場所も、`<CLI>` と同じドライブにあれば扱えます（`install` / `uninstall`
  に毎回 `--plugins-dir` で渡す。入れた場所は記録しない。[インストール](install-and-update.md#前提)）。
- `<CLI>` は、このプラグインだけでなく、`vw2026` で入れるすべてのプラグインの組み立て・退避・
  フックの場所を持ちます（`<name>` ごとに分ける）。CLI（`bin/`）・スプール（`spool/`）・PATH は
  このプラグインのフックが置きます（[インストール「`cli` のフック」](install-and-update.md#cli-のフック)）。

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
