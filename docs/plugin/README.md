# プラグインの設計

Vectorworks の中で動くプラグイン（`cli`）の設計です。全体の目的と決めたことは
[設計](../design.md)、プラグインと呼ぶ側の約束は[通信規則](../protocol.md)にあります。
ここはそれを**実装するための設計**です。

| ページ | 中身 |
| --- | --- |
| [architecture.md](architecture.md) | モジュール・入口・受け付けの流れ・ソースの配置 |
| [bridge.md](bridge.md) | 受け付け（スプール・道具の表・終了の依頼） |
| [tools.md](tools.md) | 道具の仕様 |
| [save-target.md](save-target.md) | `save` の保存先（`to`）の確かめ |
| [identifiers.md](identifiers.md) | 名前・置き場所・リリースのタグ |
| [editions.md](editions.md) | 版（Vectorworks 2026 と 2025）ごとの名前・置き場所・ビルド・リリース |
| [build-and-release.md](build-and-release.md) | CMake・CI・リリースの形 |
| [install-and-update.md](install-and-update.md) | インストールと更新（`vw2026 install`）・CLI とスプールの置き場所・`cli` のフック（PATH）・初回のスクリプト・アンインストール（`vw2026 uninstall`）。レジストリとマニフェストの書き方は [../registry.md](../registry.md) |
| [verification.md](verification.md) | 実機の確かめ方（PR のプレリリースを入れて `vw2026` で呼ぶ） |
| [windows-measurements.md](windows-measurements.md) | Windows の実機での確かめ方（起動・終了・プラグインの有無・落ちた場所） |

## 根拠

**プラグインは 1 から書きます。** 構造設計支援プラグイン
（`min-nano/vectorworks-plugin-import-ifc-homeskz`）を含め、他のプラグインのコードや決めごとは
引き継がず、ソースも移しません。根拠にするのは、
[SDK リファレンス](https://github.com/min-nano/vectorworks-developer-sdk-reference)の次のものだけです。
参照した節は各ページに示します。

| 置き場所 | 使い方 |
| --- | --- |
| [`Findings/`](https://github.com/min-nano/vectorworks-developer-sdk-reference/tree/main/Findings) | SDK の実際の挙動。無印は実機確認済み。【推定】【ヘッダ根拠】【ソース根拠】の付いたものは、実機で確かめてから頼る（[SDK の調査](../sdk-research.md)） |
| [`plugin/`](https://github.com/min-nano/vectorworks-developer-sdk-reference/tree/main/plugin) | 実機で動いているプラグイン（CMake・`Module-Info.plist.in`・`PluginPrefix.h`・リソース・無 SDK の単体テスト）。ビルドの形を確かめる見本にする |
| [`scripts/`](https://github.com/min-nano/vectorworks-developer-sdk-reference/tree/main/scripts) | SDK の取得（`fetch-vw-sdk.sh`）と CI の共通部 |

- 見本のコードは写さず、このリポジトリの決まり（[整形](../format.md)・[検査](../lint.md)）で
  書き直します。見本にしか無い仕掛け（殻と本体・メニュー・自動アップデートなど）は持ち込みません。
- **リファレンスに無いことは、実装の前に調べます**（[SDK の調査](../sdk-research.md)）。

このリポジトリのものが何を根拠にしているかは、次の通りです。

| 作るもの | 根拠 |
| --- | --- |
| `src/core/*`（Protocol・Json・Files・Bridge・Serve・Lifecycle・道具の SDK に依らない部分） | [通信規則](../protocol.md)・[ブリッジ](bridge.md)・[道具](tools.md)。SDK に依らない |
| `src/ModuleMain.cpp`・`PluginPrefix.h`・`Module-Info.plist.in`・`resources/cli.vwr`・`CMakeLists.txt` | SDK リファレンスの `plugin/`（実機で読み込まれている形）・[Findings「Plug-in Modules」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Plug-in%20Modules.md)（VCOM の初期化・SDK の静的ライブラリをリンクするモジュールが定義するもの） |
| `src/Clock.{h,cpp}` | [Findings「Timers and Notifications」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Timers%20and%20Notifications.md) の 4・6・7（[構成](architecture.md#受け付けの流れ)）。終了の依頼と起動を終えた目印は同 4 の 9（刻みから終了を頼む・「終了の呼び出しの最中」の旗・戻った後の 2 度目・起動を終えた目印。`RegisterNotificationProcedure` で `kNotifyBeforePendingUpdate` を登録する） |
| `src/tools/*` | [SDK の調査 4](../sdk-research.md#4-道具が使う-sdk-呼び出し) |
| `scripts/fetch-vw-sdk.sh` と CI の共通部 | SDK リファレンスの `scripts/`（`fetch-vw-sdk.sh`・`ci-common.sh`・`ci-wait.sh`） |
| `.github/workflows/*` | [ビルドとリリース](build-and-release.md) |
| 整形と検査の設定 | [整形](../format.md)・[検査](../lint.md) |
