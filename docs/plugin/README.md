# プラグインの設計

Vectorworks の内部で動作するプラグイン（`cli`）の設計です。全体の目的と決定事項は
[設計](../design.md)、プラグインと呼び出し元の取り決めは[通信規則](../protocol.md)にあります。
このページはそれを**実装するための設計**です。

| ページ | 内容 |
| --- | --- |
| [architecture.md](architecture.md) | モジュール・エントリポイント・受付処理の流れ・ソースの配置 |
| [bridge.md](bridge.md) | 受付処理（スプール・ツールの一覧・終了の依頼） |
| [tools.md](tools.md) | ツールの仕様 |
| [save-target.md](save-target.md) | `save` の保存先（`to`）の検証 |
| [identifiers.md](identifiers.md) | 名前・配置場所・リリースのタグ |
| [editions.md](editions.md) | 版（Vectorworks 2026 と 2025）ごとの名前・配置場所・ビルド・リリース |
| [build-and-release.md](build-and-release.md) | CMake・CI・リリースの構成 |
| [install-and-update.md](install-and-update.md) | インストールと更新（`vw2026 install`）・CLI とスプールの配置場所・`cli` のフック（PATH）・初回スクリプト・アンインストール（`vw2026 uninstall`）。レジストリとマニフェストの記法は [../registry.md](../registry.md) |
| [verification.md](verification.md) | 実機での検証方法（PR のプレリリースをインストールして `vw2026` で呼び出す） |
| [windows-measurements.md](windows-measurements.md) | Windows の実機での検証方法（起動・終了・プラグインの有無・異常終了した箇所） |

## 根拠

**プラグインは 1 から記述します。** 他のプラグインのコードや取り決めは引き継がず、ソースも移植しません。
根拠とするのは、[SDK リファレンス](https://github.com/min-nano/vectorworks-developer-sdk-reference)の次のもののみです。
参照した節は各ページに示します。

| 配置場所 | 用途 |
| --- | --- |
| [`Findings/`](https://github.com/min-nano/vectorworks-developer-sdk-reference/tree/main/Findings) | SDK の実際の挙動。無印は実機確認済み。【推定】【ヘッダ根拠】【ソース根拠】の付いたものは、実機で検証してから依拠する（[SDK の調査](../sdk-research.md)） |
| [`plugin/`](https://github.com/min-nano/vectorworks-developer-sdk-reference/tree/main/plugin) | 実機で動作しているプラグイン（CMake・`Module-Info.plist.in`・`PluginPrefix.h`・リソース・無 SDK の単体テスト）。ビルドの構成を検証する際の手本とする |
| [`scripts/`](https://github.com/min-nano/vectorworks-developer-sdk-reference/tree/main/scripts) | SDK の取得（`fetch-vw-sdk.sh`）と CI の共通部 |

- 手本のコードは複製せず、本リポジトリの規則（[整形](../format.md)・[検査](../lint.md)）で
  書き直します。手本にしか無い仕組み（殻と本体・メニュー・自動アップデートなど）は持ち込みません。
- **リファレンスに無い事項は、実装の前に調査します**（[SDK の調査](../sdk-research.md)）。

本リポジトリの成果物が何を根拠としているかは、次の通りです。

| 作成するもの | 根拠 |
| --- | --- |
| `src/core/*`（Protocol・Json・Files・Bridge・Serve・Lifecycle・ツールの SDK に依存しない部分） | [通信規則](../protocol.md)・[ブリッジ](bridge.md)・[ツール](tools.md)。SDK に依存しない |
| `src/ModuleMain.cpp`・`PluginPrefix.h`・`Module-Info.plist.in`・`resources/cli.vwr`・`CMakeLists.txt` | SDK リファレンスの `plugin/`（実機で読み込まれている形式）・[Findings「Plug-in Modules」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Plug-in%20Modules.md)（VCOM の初期化・SDK の静的ライブラリをリンクするモジュールが定義するもの） |
| `src/Clock.{h,cpp}` | [Findings「Timers and Notifications」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Timers%20and%20Notifications.md) の 4・6・7（[構成](architecture.md#受付処理の流れ)）。終了の依頼と起動完了の目印は同 4 の 9（周期処理から終了を依頼する・「終了の呼び出しの最中」のフラグ・戻った後の 2 度目・起動完了の目印。`RegisterNotificationProcedure` で `kNotifyBeforePendingUpdate` を登録する） |
| `src/tools/*` | [SDK の調査 4](../sdk-research.md#4-ツールが使用する-sdk-呼び出し) |
| `scripts/fetch-vw-sdk.sh` と CI の共通部 | SDK リファレンスの `scripts/`（`fetch-vw-sdk.sh`・`ci-common.sh`・`ci-wait.sh`） |
| `.github/workflows/*` | [ビルドとリリース](build-and-release.md) |
| 整形と検査の設定 | [整形](../format.md)・[検査](../lint.md) |
