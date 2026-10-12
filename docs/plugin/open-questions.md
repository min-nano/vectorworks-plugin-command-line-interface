# 決まっていないこと

番号は他のページから参照しています。決まったら該当のページへ書き込み、ここから消します。

## 1. 図面に書く道具の作法

書く道具（種類 `write`）を足す前に決めること。

- undo の単位（要求 1 件＝ undo 1 つ）と、その名前（`vw2026: <道具>` など）
- 人が図面を操作している最中に届いたときの扱い（OS タイマーは undo の記録が開いている間は
  見送る。ドラッグやレンダリングの最中は記録が開いていないので届く——読むだけなら問題ないが、
  書くときに人の操作と順序が混ざらないかを確かめる）
- 書いてよい対象の限定（利用者の名前付きリソースを作らない・書き換えない、など）
- 道具が開けた undo の記録は、受け付けから戻る前に閉じる。閉じずに戻ると数十秒開いたままになり、
  その間の刻みの書き込みがそこへ入る（[Findings「Timers and Notifications」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Timers%20and%20Notifications.md) の 7「落とし穴」）

## 2. undo の記録が開いている間の見送り

いまは `IsCurrentlyBuildingAnUndoEvent()` が真の間、受け付けの回ごと見送ります
（[構成「受け付けを止めない・処理中は見送る」](architecture.md#受け付けを止めない処理中は見送る)）。
ただし SDK リファレンスによると、

- 真は「利用者が操作中」を意味しない。SDK の内部（PIO の再計算・ビューポートの更新・
  スクリプトエンジンなど）が開き、スクリプトエンジンはコマンドをまたいで真を残す。文書ごとの状態でもある。
  真を見送りの条件にすると、残った記録 1 つで受け付けが止まったままになりうる
  （[Findings「Timers and Notifications」](https://github.com/min-nano/vectorworks-developer-sdk-reference/blob/main/Findings/Timers%20and%20Notifications.md) の 6「`yes` の意味が広い」）。
- 記録が開いている間も、読むだけなら安全（同 6「線引き」）。

決めること:

- 見送りを書く道具（`write`）と `quit` だけに絞り、読む道具は記録が開いていても応えるか
- 見送りが続くとき、呼ぶ側にどう見えるか（いまはタイムアウトになるだけ）

決まったことは[設計「決めたこと」](../design.md#決めたこと)と各ページにあります（ここには残しません）。
