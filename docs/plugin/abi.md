# 殻と本体の境界（`src/PayloadAbi.h`）

元:`src/PayloadAbi.h`（ABI 7）から、このプラグインに要るものだけを残した**新しい系列の版 1**
です。元のプラグインと本体を取り違えることはありません（ファイル名が違う）が、版は 1 から
数え直します。

## 決めごと

- **C の ABI だけ。** SDK・OS のヘッダを include しない。SDK の型は `void*` で渡す。
- 返す `const char*` は**次に本体を呼ぶまで・アンロードまで**有効。殻はすぐ複製する。
- **例外は境界を越えない。** 本体のエクスポート関数はすべて本体を try/catch で包み、状態コードで返す。
- **境界の形を変えたら `VW_PAYLOAD_ABI_VERSION` を上げる。** 殻と本体は別々に配られうる
  （本体だけ入れ替わる）ので、食い違いは実行時にしか分からない。殻は版が合わない本体を
  読み込まず、[殻の診断](#殻の診断shelljson)に `abi_mismatch` を書く（本体が動いていないので、
  本体の見え方や生存の印には出せない）。

## 版

```c
#define VW_PAYLOAD_ABI_VERSION 1u
```

## 殻が本体へ渡すもの

```c
struct VwPayloadHost {
    unsigned size;         // sizeof(VwPayloadHost)。二重の確かめ
    unsigned abiVersion;   // VW_PAYLOAD_ABI_VERSION
    void*    callbacks;    // SDK の CallBackPtr。本体は GS_InitializeVCOM に渡す
};
```

- 元にあった `runBundledScript`（同梱スクリプトの実行）は**持ちません**。本体に同梱
  スクリプトを走らせる用が無いため（更新は殻が行う）。要るようになったら版を上げて足します。
- 本体は受け取った構造体を `PayloadHostHolder` で複製します（元:`src/PayloadHostHolder.h`）。
  殻は `Payload` のメンバとしてアンロードまで保持します。

## 受け付け 1 回ごとに殻が渡すもの

```c
struct VwServeInput {
    unsigned    size;             // sizeof(VwServeInput)
    const char* shellReport;      // 前の回の殻に頼む道具の結果（JSON）。無ければ NULL
    int         restartRequired;  // 0 / 1。殻の ID の違う版が入っていて、再起動するまで効かない
};
```

- **殻から本体へ渡る経路は、`VwPayloadHost`（読み込み時に 1 度）とこれ（受け付けごと）だけ**です。
  見え方（view）は本体から殻への出力で、殻からは何も伝えられません。
- `restartRequired` を `VwPayloadHost` に置かないのは、**読み込んだあとで変わる**からです。殻の ID の
  違う版は Vectorworks が動いている間にも入り、そのとき殻は本体を読み直さない（[構成](architecture.md)の 6）
  ので、読み込み時に渡した値は更新されません。殻は本体の刻印の変化に気付いたときにフォルダの
  `shell-id` を読み、結果を覚えて毎回渡します。本体はこれを生存の印の `restart_required` に載せます
  （[ブリッジ](bridge.md#生存の印)）。
- 本体は `size` を確かめ、足りなければ `kVwPayloadErrHost` を返します。
- 版 1 はまだ配布していないので、`VwServeInput` は版を上げずに版 1 の形に含めます。

## 本体の素性

```c
struct VwPayloadInfo {
    unsigned    size;
    const char* version;   // VW_BUILD_VERSION（短い sha）
    const char* branch;    // VW_BUILD_BRANCH
};
```

## エクスポートする関数

| 名前 | 型 | 役目 |
| --- | --- | --- |
| `vw_payload_abi_version` | `unsigned (void)` | 版を返す。殻はまずこれを確かめる |
| `vw_payload_init` | `int (const VwPayloadHost*)` | 受け取りを複製し、VCOM を初期化する |
| `vw_payload_info` | `int (VwPayloadInfo*)` | 素性を返す |
| `vw_payload_serve` | `int (const VwServeInput*, const char** out)` | 受け付けを 1 回行い、見え方（view）の JSON を `out` へ |
| `vw_payload_shutdown` | `void (void)` | アンロードの直前。生存の印は消さない（下記） |

- 殻はこの 5 つを**すべて必須**として解決します。1 つでも無ければ読み込み失敗です。
- 元にあった `vw_payload_run_import` / `run_test` / `recalculate` と PIO の種別は持ちません。

### `vw_payload_shutdown` で生存の印を消さない理由

アンロードは**入れ替え**のときにも起きます。そこで印を消すと、入れ替えの間に呼ぶ側が
「止まった」と判断します。Vectorworks が終了すれば印の `pid` のプロセスが無くなり「止まっている」と判定されるので、本体は印を消しません（[ブリッジ](bridge.md#生存の印)）。

## 状態コード

```c
enum VwPayloadStatus {
    kVwPayloadOk = 0, kVwPayloadErrAbi = 1, kVwPayloadErrHost = 2, kVwPayloadErrVcom = 3,
    kVwPayloadErrNotInit = 4, kVwPayloadErrException = 5
};
```

## `vw_payload_serve` の約束（殻に頼む道具）

Vectorworks の終了は、本体のコードがスタックに載っている間に頼めないので殻が行います（元の M38・ABI 7 と同じ仕組み。元は更新もここで行ったが、このプラグインでは更新は CLI が行う）。

1. 本体は要求の中に殻に頼む道具（[道具の表](bridge.md#道具の表)の種類 `shell`）を見つけたら、
   **応答を書かずに**見え方へ `"action":{"id","tool","args"}` を載せて返す。1 回の呼び出しで
   受け取るのは 1 件だけで、2 件目には「別の頼みごとを処理しています」と失敗で応える。
2. 殻は action を実行し、結果 `{"id","ok","result","error"}` を次の呼び出しの
   `VwServeInput::shellReport` に渡す。
3. **そのとき読み込まれている本体**（入れ替えたなら新しい本体）が応答を書き、見え方に
   `"reportDone":true` を立てる。結果には書いた本体の `payload_version` を添える。
4. 殻は `reportDone` が立つまで（最長 120 秒）同じ結果を渡し直す。

## 殻の診断（`shell.json`）

殻の側の失敗（本体を読み込めない・ABI の版が合わない・初期化に失敗した）は、本体が動いて
いないので、見え方にも生存の印にも出せません。パレットも無いので、そのままでは
`vw2026 status` が `live:false` を返すだけになり、「Vectorworks が動いていない」と区別できません。
そこで**殻は、本体を動かせない間だけ、スプールに `shell.json` を書きます**（形は
[作法「殻の診断」](../protocol.md#殻の診断shelljson)）。殻がスプールに触れるのはこのファイルだけです。

| `error.code` | いつ |
| --- | --- |
| `copy_failed` | 本体を一時ディレクトリへ複製できない |
| `load_failed` | `dlopen` / `LoadLibrary` が失敗した（`message` に OS の理由） |
| `symbol_missing` | エクスポート関数のどれかが無い |
| `abi_mismatch` | `vw_payload_abi_version` が殻の `VW_PAYLOAD_ABI_VERSION` と違う |
| `init_failed` | `vw_payload_init` が `kVwPayloadOk` 以外を返した |
| `serve_failed` | `vw_payload_serve` が `kVwPayloadOk` 以外を返した |

- **書く・消す時機**: 失敗している間は、生存の印と同じく 2 秒ごとに書き直します（`beat`）。
  `vw_payload_serve` が 1 度でも `kVwPayloadOk` を返したら消します。Vectorworks が終わると書き
  直されなくなり、15 秒で古びて呼ぶ側に無視されます。
- **読み直し**: 失敗のあとも、本体の刻印が変わったら（入れ直された・更新された）読み込みを
  やり直します。刻印が変わらない間は読み込みを繰り返しません（毎回の `dlopen` の失敗を避ける）。
- **場所**: 本体と同じ規則で決めます（`VW2026_SPOOL`、無ければ `<一時ディレクトリ>/<PLUGIN_SPOOL>`）。
  規則と持ち主・権限の確かめを殻と本体で食い違わせないよう、`src/SpoolDir.{h,cpp}`（SDK にも
  `src/core/` にも依らない）を両方にリンクします。無ければ 0700 で作り、確かめに通らない場所には
  書きません（何も伝えられないが、`status` の `searched` に理由が出る）。
- **JSON は殻の中で決まった形を書くだけ**です（`message` の文字列の逃がしだけを持ち、JSON の
  ライブラリは使わない）。`message` は英語（OS の理由をそのまま入れる）。
- **案内は呼ぶ側が行います。** 殻は事実（`code` と `message`）だけを書き、「入れ直してください」
  などの解釈は持ちません（`vw2026 status` はそのまま `searched[].shell` に載せる）。
