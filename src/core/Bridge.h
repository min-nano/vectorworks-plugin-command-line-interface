//
// core/Bridge.h
//
// **スプール 1 つ。** 受け渡しの作法（docs/protocol.md）のうち、ファイルを扱う部分——
// 場所・ロック・掃除・要求の取り出し・待つ印・応答——と、要求と応答の形を持つ。受け付け
// 1 回の手順は core/Serve.h にある。SDK に依らず、OS の呼び出しだけで書くので、無 SDK で
// テストできる（tests/CoreBridgeTests.cpp）。
//
//     vw2026 / ほかのプログラム
//            │ <id>.wait を掴む → <id>.req.json を置く／<id>.res.json を待つ
//            ▼
//     スプール（<CLI>/spool。0700）── bridge.lock（受け付けている間ずっと掴む）
//            ▲
//            │ 取り出す／応答する（OS タイマーが受け付けを 1 回ずつ呼ぶ）
//     Vectorworks（core::Server）
//
// 【定数】作法の定数はこのファイルと cli/internal/spool が持つ。どちらのテストも
// protocol/fixtures/constants.json と照らすので、片側だけ変えると落ちる。
//
// 【子プロセスに受け継がせない】開くファイル（bridge.lock・<id>.wait・要求・応答）は、
// POSIX では O_CLOEXEC を付けて、Windows では継承できないハンドルで開く。Vectorworks が
// 起動した子プロセスに bridge.lock を受け継がれると、Vectorworks が終わってもロックが
// 放されず、止まったと判定されなくなる（docs/protocol.md「ロック」）。std::fstream は
// これを指定できないので使わない。
//
// 【パス】std::filesystem::path で持つ。Windows でも利用者の名前（日本語を含みうる）を
// ANSI のコードページに通さないため。
//

#pragma once

#include "core/Json.h"

#include <cstddef>
#include <filesystem>
#include <string>
#include <vector>

namespace VwCli::core {
// 作法の版（docs/protocol.md「版」）。表示のためだけで、実行時に照合しない。
inline constexpr int kBridgeProtocolVersion = 5;

// <CLI> のディレクトリ名と、その下のスプールの名前（docs/plugin/identifiers.md「置き場所」）。
// cli/internal/spool の AppDirName / SpoolDirName と対。
inline constexpr const char* kBridgeAppDirName = "vectorworks2026-cli";
inline constexpr const char* kBridgeSpoolDirName = "spool";

// スプールに置くファイルの綴り（docs/protocol.md「ファイル」）。
inline constexpr const char* kBridgeLockFile = "bridge.lock";
inline constexpr const char* kBridgeRequestSuffix = ".req.json";
inline constexpr const char* kBridgeResponseSuffix = ".res.json";
inline constexpr const char* kBridgeWaitSuffix = ".wait";
inline constexpr const char* kBridgeTempSuffix = ".tmp";

// 要求 1 件の大きさの上限（バイト）と、入れ子の深さの上限（docs/protocol.md「要求」）。
inline constexpr std::size_t kBridgeMaxRequestBytes = std::size_t{1} << 20U;
inline constexpr int kBridgeMaxNestingDepth = kJsonMaxDepth;

// id の長さの上限（docs/protocol.md「id」）。
inline constexpr std::size_t kBridgeMaxIdLength = 64;

// スプールの場所（<CLI>/spool。docs/protocol.md「スプールの場所」）。環境変数は読まず、
// 探索しない。mac（と、テストのための Linux）は利用者の passwd の項目のホームの
// Library/Application Support、Windows は FOLDERID_LocalAppData の下。求められなければ
// false と理由。
bool bridgeSpoolDir(std::filesystem::path& out, std::string& error);

// 求めたホーム（mac）または LocalAppData（Windows）の下のスプール。bridgeSpoolDir の
// 組み立ての部分で、テストが照らせるように分けてある。
std::filesystem::path bridgeSpoolDirUnder(const std::filesystem::path& base);

// id としてファイル名に使ってよい綴りか（[0-9A-Za-z_-]{1,64}）。**スプールの外へ
// 書かせないための関門**である。
bool isValidBridgeId(const std::string& id);

// UTF-8 の文字列 ↔ パス。Windows でも ANSI のコードページを通さない。
std::filesystem::path pathFromUtf8(const std::string& text);
std::string utf8FromPath(const std::filesystem::path& path);

// -----------------------------------------------------------------------
// 失敗の種別（応答の code。docs/protocol.md「応答」）。綴りは作法で固定する。
enum class BridgeCode {
    InvalidRequest,
    UnknownTool,
    InvalidArgs,
    NoWait,
    Busy,
    NoSession,
    Internal,
};

// code の綴り（invalid_request など）。
const char* bridgeCodeName(BridgeCode code);

// 要求 1 件（docs/protocol.md「要求」）。id はファイル名だけが持つ。
struct BridgeRequest {
    std::string tool;
    // 道具の引数。省略・null なら {}。
    Json args = Json::object();
    // 占有の印。省略・null・空の文字列なら空。
    std::string session;
};

// 要求のテキスト → BridgeRequest。読めない要求（invalid_request）なら false と理由。
// 大きさの上限は呼ぶ側（取り出すとき）が確かめる。
bool parseBridgeRequest(const std::string& text, BridgeRequest& out, std::string& error);

// 応答 1 件（docs/protocol.md「応答」）。ok が false のときだけ code と error を書く。
struct BridgeResponse {
    bool ok = false;
    BridgeCode code = BridgeCode::Internal;
    std::string error;
    Json result;

    static BridgeResponse success(Json result);
    static BridgeResponse failure(BridgeCode code, std::string error);
};

// 応答 → JSON テキスト。成功で result が null なら {} を書く。
std::string dumpBridgeResponse(const BridgeResponse& response);

// -----------------------------------------------------------------------
// 呼ぶ側の待つ印（<id>.wait）の状態（docs/protocol.md「待つ印」）。
enum class WaitState {
    // 掴めない。呼ぶ側が待っている
    Held,
    // 掴めた。呼ぶ側が放した・死んだ
    Released,
    // 無い（開けない）
    Missing,
};

// ロックを取りに行った結果。
enum class LockResult {
    Locked,
    // ほかが掴んでいる（別の Vectorworks か、確かめている呼ぶ側）
    HeldElsewhere,
    Failed,
};

// 要求を 1 件取り出した結果。
enum class TakeResult {
    // 読んで消した
    Taken,
    // 上限を超えていた（読まずに消した）
    TooLarge,
    // 読めない（無くなっていた）・消せない。黙って飛ばす
    Skipped,
};

// **スプール 1 つ。** 場所は引数で受ける（テストは一時ディレクトリを渡す）。ロックは
// このオブジェクトが持ち、壊すときに放す。
class BridgeSpool {
public:
    explicit BridgeSpool(std::filesystem::path dir);
    ~BridgeSpool();
    BridgeSpool(const BridgeSpool&) = delete;
    BridgeSpool& operator=(const BridgeSpool&) = delete;
    BridgeSpool(BridgeSpool&&) = delete;
    BridgeSpool& operator=(BridgeSpool&&) = delete;

    const std::filesystem::path& dir() const {
        return fDir;
    }

    // 無ければ作る（親も。POSIX は 0700）。持ち主と権限は確かめない。
    bool prepare(std::string& error);

    // bridge.lock を掴む。取れたら放すまで（壊すまで）持つ。掴んでいれば何もしない。
    LockResult lock(std::string& error);
    bool locked() const;
    // 放す（Vectorworks の終了を真似るテストのため。プラグインは呼ばない）。
    void unlock();

    // ロックを取れた直後の掃除。要求・応答・待つ印・書きかけのうち、待つ印を掴める・
    // 無い id のものだけを消す（id はファイル名の最初の '.' より前）。消した数を返す。
    std::size_t sweep();

    // 置かれている要求の id を名前の昇順で並べる。綴りを満たさない id は並べない。
    std::vector<std::string> poll() const;

    // 要求を 1 件読んでから消す。消せたものだけを Taken / TooLarge で返す。
    TakeResult take(const std::string& id, std::string& text);

    // 待つ印の状態。掴めたら放して返す（消すのは removeWait）。
    WaitState waitState(const std::string& id) const;
    void removeWait(const std::string& id);

    // 応答を書く（<id>.res.json.tmp へ書いてから rename する）。
    bool reply(const std::string& id, const BridgeResponse& response, std::string& error);

private:
    std::filesystem::path fDir;
#if defined(_WIN32)
    // 掴んでいる bridge.lock のハンドル（HANDLE。windows.h をここで読まないため void*）
    void* fLock = nullptr;
#else
    // 掴んでいる bridge.lock の記述子
    int fLock = -1;
#endif

    std::filesystem::path pathFor(const std::string& name) const;
};
} // namespace VwCli::core
