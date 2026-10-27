# プラグインの設計

Vectorworks の中で動くプラグイン（`cli`）の設計です。全体の目的と決めたことは
[設計](../design.md)、プラグインと呼ぶ側の約束は[通信規則](../protocol.md)にあります。
ここはそれを**実装するための設計**で、実装は別のセッションで行います。

**プラグインは 1 から書きます。** 構造設計支援プラグイン
（`min-nano/vectorworks-plugin-import-ifc-homeskz`）のコードや決めごとは引き継ぎません。
根拠にするのは、[SDK リファレンス](https://github.com/min-nano/vectorworks-developer-sdk-reference)
にまとめられた確かめられた仕様と挙動だけです（下記「根拠」）。

| ページ | 中身 |
| --- | --- |
| [architecture.md](architecture.md) | モジュール・入口・受け付けの流れ・ソースの配置 |
| [bridge.md](bridge.md) | 受け付け（スプール・道具の表・終了の依頼） |
| [tools.md](tools.md) | 最初に提供する道具の仕様 |
| [identifiers.md](identifiers.md) | 名前・置き場所・リリースのタグ |
| [build-and-release.md](build-and-release.md) | CMake・CI・リリースの形 |
| [install-and-update.md](install-and-update.md) | インストールと更新（`vw2026 install`）・CLI とスプールの置き場所・`cli` のフック（PATH）・初回のスクリプト・アンインストール（`vw2026 uninstall`）。レジストリとマニフェストの書き方は [../registry.md](../registry.md) |
| [plan.md](plan.md) | 実装の順序（1 段 1 PR）・各段の終わりの確かめ方 |
| [open-questions.md](open-questions.md) | 決まっていないこと |
| [windows-measurements.md](windows-measurements.md) | Windows の実機での実測（段 2。起動・終了・プラグインの有無） |

## 根拠

設計と実装の根拠は SDK リファレンスの次のものです。参照した節は各ページに示します。

| 置き場所 | 使い方 |
| --- | --- |
| [`Findings/`](https://github.com/min-nano/vectorworks-developer-sdk-reference/tree/main/Findings) | SDK の実際の挙動。無印は実機確認済み。【推定】【ヘッダ根拠】【ソース根拠】の付いたものは、実機で確かめてから頼る（[SDK の調査](../sdk-research.md)） |
| [`plugin/`](https://github.com/min-nano/vectorworks-developer-sdk-reference/tree/main/plugin) | 実機で動いているプラグイン（CMake・`Module-Info.plist.in`・`PluginPrefix.h`・リソース・無 SDK の単体テスト）。ビルドの形を確かめる見本にする |
| [`scripts/`](https://github.com/min-nano/vectorworks-developer-sdk-reference/tree/main/scripts) | SDK の取得（`fetch-vw-sdk.sh`）と CI の共通部 |

- 見本のコードは写さず、このリポジトリの決まり（[整形](../format.md)・[検査](../lint.md)）で
  書き直します。見本にしか無い仕掛け（殻と本体・メニュー・自動アップデートなど）は持ち込みません。
- **リファレンスに無いことは、実装の前に調べます**（[SDK の調査](../sdk-research.md)）。
