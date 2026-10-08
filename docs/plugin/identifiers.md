# 名前と識別子

**ユニバーサル名と拡張機能の UUID は、最初の配布のあとは付け替えません**（コマンドの同一性
そのもので、付け替えるとワークスペースからコマンドが消える）。スプールの名前・コマンド名・
リンクの名前も、変えると呼ぶ側が見つけられなくなるので同じ扱いです。

| 項目 | 安定版 | 開発版 |
| --- | --- | --- |
| プラグイン名（ファイル名・フォルダ名・`PLUGIN_VWR_ID`） | `cli` | `cliDev` |
| 表示名（メニューのカテゴリ） | 未決（[未決 2](open-questions.md)） | 同じ＋`Dev` |
| 殻 | `cli.vwlibrary` / `cli.vlb` | `cliDev.vwlibrary` / `cliDev.vlb` |
| 本体 | `cli.vwpayload` | `cliDev.vwpayload` |
| リソース | `cli.vwr` | `cliDev.vwr` |
| バンドル ID（mac） | `io.github.min-nano.cli` | `io.github.min-nano.cli-dev` |
| スプール | `<temp>/vectorworks-cli-bridge` | `<temp>/vectorworks-cli-bridge-dev` |
| コマンド | `vw2026` | `vw2026`（`--channel dev` で開発版の橋へ） |
| CMake のターゲット | `VwCli` / `VwCliPayload` | `VwCliDev` / `VwCliDevPayload` |
| リリースのタグ | `stable` | `dev-<ブランチの slug>` |
| インストール先 | `<Plug-Ins>/cli/` | `<Plug-Ins>/cliDev/` |

## メニューコマンド

| コマンド | クラス | ユニバーサル名 | UUID（安定版） | UUID（開発版） |
| --- | --- | --- | --- | --- |
| アップデートを確認 | `CExtMenuCheckUpdate` | `CExtMenuCheckUpdate_VwCli` / `…_VwCliDev` | `5ab5a377-7c80-46c9-91ae-9800126c6704` | `b0465dca-0bc8-40eb-b658-17a57196cc25` |
| CLI ブリッジの状態 | `CExtMenuBridgeStatus` | `CExtMenuBridgeStatus_VwCli` / `…_VwCliDev` | `0c04c696-5d70-4d17-bf30-f81d3ce42811` | `18b54000-a4ca-4cad-aa89-b95441d0dbc0` |

- メニューのカテゴリは `.vwr` の `"category"` 1 つを全メニュー定義が引きます（元と同じ）。
- 開発版は安定版と同居できるよう、名前・ユニバーサル名・UUID・バンドル ID・スプールを
  すべて別にします。

## `src/BuildConfig.h`

元:`src/BuildConfig.h` と同じ形で、`VW_DEV_BUILD` の有無で切り替えます。

```cpp
#ifdef VW_DEV_BUILD
#  define PLUGIN_VWR_ID   "cliDev"
#  define PLUGIN_CHANNEL  "dev"
#  define PLUGIN_SPOOL    "vectorworks-cli-bridge-dev"
#else
#  define PLUGIN_VWR_ID   "cli"
#  define PLUGIN_CHANNEL  "stable"
#  define PLUGIN_SPOOL    "vectorworks-cli-bridge"
#endif
// VW_BUILD_VERSION / VW_BUILD_BRANCH / VW_SHELL_ID は CMake が渡す（無ければ "local"）
```

スプールの名前は CLI 側の `cli/internal/spool` の `StableSpoolName` / `DevSpoolName` と対です
（[作法](../protocol.md#スプールの場所)）。
