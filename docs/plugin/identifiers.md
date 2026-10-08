# 名前と識別子

**スプールの名前・コマンド名・リンクの名前・インストール先は、最初の配布のあとは変えません**
（変えると呼ぶ側や更新が見つけられなくなる）。

| 項目 | 安定版 | 開発版 |
| --- | --- | --- |
| プラグイン名（ファイル名・フォルダ名・`PLUGIN_VWR_ID`） | `cli` | `cli_dev` |
| 殻 | `cli.vwlibrary` / `cli.vlb` | `cli_dev.vwlibrary` / `cli_dev.vlb` |
| 本体 | `cli.vwpayload` | `cli_dev.vwpayload` |
| リソース | `cli.vwr` | `cli_dev.vwr` |
| バンドル ID（mac） | `io.github.min-nano.cli` | `io.github.min-nano.cli-dev`（バンドル ID に `_` は使えない） |
| スプール | `<temp>/vectorworks2026-cli-bridge` | `<temp>/vectorworks2026-cli-bridge-dev` |
| コマンド | `vw2026` | `vw2026`（`--channel dev` で開発版の橋へ） |
| CMake のターゲット | `VwCli` / `VwCliPayload` | `VwCliDev` / `VwCliDevPayload` |
| リリースのタグ | `stable` | `dev-<ブランチの slug>` |
| インストール先 | `<Plug-Ins>/cli/` | `<Plug-Ins>/cli_dev/` |

## 拡張機能

**登録しません**（[構成](architecture.md#拡張機能を登録しない)）。拡張機能なしで読み込まれない
ことが実機で分かったときだけ、メニューコマンドを 1 つ登録し、そのユニバーサル名と UUID を
ここに足します（安定版と開発版で別々にする）。

## `src/BuildConfig.h`

元:`src/BuildConfig.h` と同じ形で、`VW_DEV_BUILD` の有無で切り替えます。

```cpp
#ifdef VW_DEV_BUILD
#  define PLUGIN_VWR_ID   "cli_dev"
#  define PLUGIN_CHANNEL  "dev"
#  define PLUGIN_SPOOL    "vectorworks2026-cli-bridge-dev"
#else
#  define PLUGIN_VWR_ID   "cli"
#  define PLUGIN_CHANNEL  "stable"
#  define PLUGIN_SPOOL    "vectorworks2026-cli-bridge"
#endif
// VW_BUILD_VERSION / VW_BUILD_BRANCH / VW_SHELL_ID は CMake が渡す（無ければ "local"）
```

スプールの名前は CLI 側の `cli/internal/spool` の `StableSpoolName` / `DevSpoolName` と対です
（[作法](../protocol.md#スプールの場所)）。
