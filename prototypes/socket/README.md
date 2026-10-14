# 試作: ソケットでの受け渡し

**マージしない試作です。** スプールのファイルでの受け渡し（[作法](../../docs/protocol.md)、
protocol 5）を AF_UNIX のストリームソケットに置き換えると単純になるかを判断するため、前提を
macOS と Windows の CI で確かめ、置き換わる範囲の行数を比べます。

- C++（プラグイン側）: `src/Socket*.{h,cpp}`（OS の呼び出し）・`src/SocketServe.{h,cpp}`
  （受け付け 1 回）・`tests/SocketTests.cpp`。ルートの `CMakeLists.txt` の `SocketPrototypeTests`
  で、`test.yml` が macOS と Windows で走らせる。
- Go（呼ぶ側）: `cli/internal/sockproto`。`cli.yml` が macOS と Windows で走らせる。

## 手順

1. 呼ぶ側は `<spool>/bridge.sock` につなぎ、要求の JSON を 1 行（改行で終わる）で送る。
2. プラグインは受け付けの回ごとに、待っている接続をつないだ順にすべて受け取り、読めるだけ読む。
   改行まで届いた要求について:
   1. 要求の後ろにもう何か届いている・閉じられている（EOF・切断）なら、呼ぶ側は待つのをやめた・
      死んだ。`no_wait` で応え、実行しない（作法 5 の待つ印の代わり）。
   2. 受け取りの行 `{"taken":true}` を送る。
   3. 送った**あと**に 1 バイト読めた・閉じられていたら、呼ぶ側は待つのをやめた・死んだ。
      `no_wait` で応え、実行しない。
   4. 実行し、応答の JSON を 1 行送って閉じる。送り切れない分は次の回に続ける。占有の印と終了の
      依頼は、送り切ったときに反映する。
3. 呼ぶ側は、締切までに受け取りの行が届かなければ 1 バイト送り、届いている受け取りの行を読む。
   無ければ「実行されない」。受け取りの行があれば、猶予だけ応答を待ち、届かなければ「実行された
   かもしれない」。

3 の判定は、同じ計算機の中のソケットでは、送った時点でデータが相手の受け取り口に入ることに依る。
プラグインは 2-2 で受け取りの行を送ってから 2-3 で 1 バイトを見るので、呼ぶ側の 1 バイトが 2-3 に
間に合わなければ、受け取りの行はそれより前に呼ぶ側の受け取り口に入っている。

`bridge.lock` は残す。2 つの Vectorworks のどちらが受け付けるかと、前の Vectorworks が残した
ソケットのファイルを消してよいかを、ロックで決める。

## 確かめること

| 前提 | テスト | Linux | macOS | Windows |
| --- | --- | --- | --- | --- |
| 閉じた呼ぶ側へ送ると失敗し、SIGPIPE で落ちない | `SocketClosedClientIsNotRun` | 確かめた | CI | CI |
| 止まらずに数回へ分けて送る（8 MiB） | `SocketLargeResponseSpansTicks` | 40 回 | CI | CI |
| 要求が数回に分かれて届いても読める | `SocketPartialRequestWaitsForNewline` | 確かめた | CI | CI |
| やめた呼ぶ側を実行しない | `SocketCancelBeforeTakenIsNotRun`・Go `TestCallDeferredIsNotRun` | 確かめた | CI | CI |
| 送る側だけを閉じた呼ぶ側も、やめたものとして実行しない | `SocketHalfClosedClientIsNotRun` | 確かめた | CI | CI |
| つないだ順に実行する | `SocketRunsInConnectOrder` | 確かめた | CI | CI |
| 残ったソケットを次の側が作り直せる・ロックの無い側は触らない | `SocketStaleSocketIsReplaced`・`SocketSecondServerLeavesSocketAlone` | 確かめた | CI | CI |
| パスに ASCII でない文字があってもよい（Windows が `sun_path` を UTF-8 で読むか） | `SocketNonAsciiPath`・Go `TestCallNonASCIIPath` | 確かめた | CI | CI |
| パスの長さの上限と、スプールの余裕 | `SocketPathLimit`・Go `TestSocketPathLength`（ログに出す） | 107 バイト | CI | CI |

確かめていないこと:

- 2-3 と 3 の間の競合（受け取りの行を送った直後に呼ぶ側がやめる）は、テストで再現できない。
  Windows の AF_UNIX が送った時点で相手の受け取り口に入れるかは、文書が見当たらない。
- C++ のプラグイン側と Go の呼ぶ側を直接つなぐテストは書いていない（両側とも同じ OS のソケット）。
- 実機の Vectorworks の中で、OS タイマーの刻みからソケットを扱うこと。

## 結果

CI の結果を待って書く。
