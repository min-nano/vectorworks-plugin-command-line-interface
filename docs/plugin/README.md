# プラグインの設計

Vectorworks の中で動くプラグイン（`cli`）の設計です。全体の目的と決めたことは
[設計](../design.md)、プラグインと呼ぶ側の約束は[受け渡しの作法](../protocol.md)にあります。
ここはそれを**実装するための設計**で、実装は別のセッションで行います。

**構造設計支援プラグイン**（[min-nano/vectorworks-plugin-import-ifc-homeskz](https://github.com/min-nano/vectorworks-plugin-import-ifc-homeskz)。
以下「元のプラグイン」）で実機で確かめた作りを、決めごとごと引き継ぎます。元のプラグインの
パスは `元:src/…` と書きます（HEAD 6ffd221 時点）。

| ページ | 中身 |
| --- | --- |
| [architecture.md](architecture.md) | モジュール・入口・受け付けの流れ・ソースの配置 |
| [bridge.md](bridge.md) | 受け付け（スプール・道具の表・終了の依頼） |
| [tools.md](tools.md) | 最初に提供する道具の仕様 |
| [identifiers.md](identifiers.md) | 名前・置き場所・リリースのタグ |
| [build-and-release.md](build-and-release.md) | CMake・CI・リリースの形 |
| [install-and-update.md](install-and-update.md) | インストールと更新（`vw2026 install`。PATH を含む）・初回のスクリプト・アンインストール（`vw2026 uninstall`） |
| [plan.md](plan.md) | 実装の順序（1 段 1 PR）・各段の終わりの確かめ方・元のプラグインから移すもの |
| [open-questions.md](open-questions.md) | 決まっていないこと |
