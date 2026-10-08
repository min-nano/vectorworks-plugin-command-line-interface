# SDK の調査（待っているもの）

**SDK の挙動が分からないまま実装に入らない**のがこのリポジトリの決めごとです。調べるのは
[SDK リファレンス](https://github.com/min-nano/vectorworks-developer-sdk-reference)の側で、
issue を立てて `Findings/` に反映されてから、それを根拠に実装します。待つ間は SDK に依らない
作業（作法・CLI・殻と本体の移植）を進めます。

## 1. ほかのプラグインの機能をユニバーサル名で呼び出す手段

[設計「他のプラグインの道具」](design.md#他のプラグインの道具)のため。調査は
[SDK リファレンス #217](https://github.com/min-nano/vectorworks-developer-sdk-reference/issues/217)。

調べる経路（宣言は `SDK Index/` にあることを確かめた。挙動は未確認）:

| 経路 | 宣言 |
| --- | --- |
| プラグインのライブラリ関数を名前で呼ぶ | `ISDK::CallPluginLibrary(routineName, PluginLibraryArgTable*, status)` |
| メニューコマンドをユニバーサル名で起動する | 未発見（`DoMenuTextByName` は表示名と番号） |
| 内蔵スクリプトエンジンで実行する | `IVectorScriptEngine::ExecuteScript` / `IPythonScriptEngine::ExecuteScript` |
| 相手の殻の公開関数を引く（C の ABI） | `dlsym` / `GetProcAddress` |

併せて、呼べる時機（OS タイマーの中・undo の記録）、相手が居ないときの振る舞い
（`HasPlugin`）、読み込み順を確かめる。

## 2. 汎用の実行口（のちの検討）

[設計「進め方」](design.md#進め方)の 6（書く道具）を汎用にするかの判断材料。

- プラグインから Vectorworks 内蔵の Python／VectorScript を、文字列で渡して実行できるか。
  結果や出力を受け取れるか。
- メニューコマンドを名前（ユニバーサル名）で起動できるか。
- Vectorworks 自身の外部からの受け口（macOS の Apple Events、起動引数でのスクリプト実行など）が
  あるか。
