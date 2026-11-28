#pragma once

// 1 回の受付処理の手順と、占有のセッション ID（docs/plugin/bridge.md「1 回の受付処理」）。SDK に
// 依存しない。ツールの一覧と時刻を引数で受け取るため、擬似ツールの一覧でテストできる。

#include "core/Bridge.h"
#include "core/Json.h"

#include <cstdint>
#include <optional>
#include <span>
#include <string>
#include <string_view>

namespace VwCli::core {

// ツールの種類（@tools の結果の kind）。Read は図面を変更せず、占有と照合しない。Write は図面の
// 内容を変更する（図面を変更するか否かが不明なツールも Write とする）。App は Vectorworks・図面の
// ファイルやウィンドウに作用し、図面の内容は変更しない（save・close・quit など）。Write と App は
// 占有の中でのみ呼び出せる。終了の依頼は種類では決定せず、ツールが ToolStatus::quit で返す。
enum class ToolKind { Read, Write, App };

// 受付処理の外の状態。タイマー（Clock）が回ごとに渡す。Quitting では、ツールの一覧のツールを全て
// busy で拒否する（種類 Read と ping も）。SDK を呼び出さない 3 つのツール（@tools・
// @session_start・@session_end）と、一覧に無いツールの unknown_tool は通常通り応答する
// （docs/plugin/architecture.md「Vectorworks が処理中」）。
enum class Activity {
	Idle,
	// 終了の依頼の処理中（保存の確認を表示している）。
	Quitting,
};

// ツールが返す失敗の種別。None は失敗していない。
enum class ErrorCode { None, InvalidArgs, Internal };

// ツールが返す失敗と終了の依頼。
struct ToolStatus {
	ErrorCode code = ErrorCode::None;
	// 日本語。応答の error にそのまま含まれる。
	std::string message;
	// 失敗の詳細（JSON のオブジェクト）。失敗した場合のみ応答の detail にそのまま含まれる（通信規則
	// 「応答」）。空（null）なら detail を付けない。オブジェクトでない値は internal で応答する。
	Json detail;
	// 終了の依頼。成功し、応答を書き込めた場合に、受付処理の外で終了を依頼する（quit のみが
	// 設定する）。
	bool quit = false;
};

// 起動の状態（受付処理の外。タイマーが core/Lifecycle.h から渡し、quit が参照する）。
struct Startup {
	// Vectorworks が起動を終えたか（最初の kNotifyBeforePendingUpdate で設定される）。
	bool started = false;
	// その通知を登録できたか（できない間は started が設定されない。quit が「起動の途中」と
	// 「登録できていない」を呼び出し元に区別して示すため）。
	bool notificationRegistered = false;
};

// ツールに渡す受付処理の状態。
struct ToolContext {
	// 占有のセッション ID を発行しているか（ping の session）。
	bool session = false;
	Startup startup;
};

// ツールの内容。結果は JSON のオブジェクトで返す（返す値が無ければ Json() でよく、{} になる。
// オブジェクトでない値は internal で応答する。通信規則「応答」）。失敗と終了の依頼は status に
// 書き込み（失敗した場合の戻り値は使用しない）、例外は Server が internal で応答する。
using ToolFn = Json (*)(const Json &args, const ToolContext &context, ToolStatus &status);

// ToolSpec はツールの一覧の 1 行の内 SDK に依存しない部分（名前・説明・inputSchema・種類）。
// ツールごとに src/core に定数で配置し、src/tools/ToolTable.cpp が SDK に依存する内容と組み合わせる
// （種類を取り違えて占有なしで呼び出せるようにしないことを、無 SDK のテストで検証するため）。
struct ToolSpec {
	// [a-z0-9_]+(\.[a-z0-9_]+)*。通信規則の綴り（core/Protocol.h の std::string_view）をそのまま
	// 使用できるように、string_view で保持する。
	std::string_view name;
	// 日本語。呼び出し元がそのまま読み取る。
	const char *description;
	// JSON Schema（文字列）。@tools の結果にそのまま含まれる。
	const char *inputSchema;
	ToolKind kind;
};

// Tool はツールの一覧の 1 行（ToolSpec と内容）。
struct Tool : ToolSpec {
	ToolFn run;
};

// toolOf は ToolSpec と内容から一覧の 1 行を作成する。
constexpr Tool toolOf(const ToolSpec &spec, ToolFn run) { return Tool{spec, run}; }

// ScopedFlag はフラグを設定し、破棄時に解除する（例外で抜ける場合も解除する）。
class ScopedFlag {
public:
	explicit ScopedFlag(bool &flag) : flag_(flag) { flag_ = true; }
	~ScopedFlag() { flag_ = false; }
	ScopedFlag(const ScopedFlag &) = delete;
	ScopedFlag &operator=(const ScopedFlag &) = delete;

private:
	bool &flag_;
};

// 受付処理の状態。
enum class Phase {
	// ロックを取得して受付処理を行った
	Serving,
	// 他の Vectorworks がロックを保持しているため、何もしなかった
	Waiting,
	// スプールを準備できない・ロックのファイルを開けない（次の回に再試行する）
	Error,
};

struct ServeResult {
	Phase phase = Phase::Serving;
	// Error の理由。
	std::string message;
	// ツールが成功して ToolStatus::quit を設定し、その応答を書き込めた（終了の依頼）。受付処理の
	// 外で終了を依頼する。
	bool quit = false;
};

// Server はスプール 1 つの受付処理と、発行した占有のセッション ID（1 つのみ。メモリのみに
// 保持する）。
class Server {
public:
	explicit Server(std::string spoolDir);

	// serve は 1 回の受付処理。nowNanos は Unix 時刻のナノ秒で、占有のセッション ID の先頭に
	// 使用する。
	//
	// ロックを保持していなければ準備（prepare → lock → sweep）を行い、取得できなければ何もせずに
	// 戻る。回の始めに列挙した要求を名前の昇順で全て処理し、取り出した要求には同じ回の中で応答する
	// （受付処理の入れ子が発生した場合は、入れ子の回が先に取り出して拒否した要求を飛ばす）。
	// ツールが成功して終了の依頼（ToolStatus::quit）を設定し、応答を書き込めたら、その回の残りの
	// 要求を取り出さずに戻る（終了したら応答できないため、残りは配置したままにする）。
	//
	// ツールの中で Vectorworks がイベントを処理し、serve の途中で再度 serve が呼び出されたら
	// （受付処理の入れ子）、外の回の状態に触れず、取り出せる要求を全て busy で拒否して戻る。外の回
	// が既に列挙していた要求も拒否する（外の回の一覧を入れ子の側へ公開する状態を保持しないため）。
	//
	// activity と startup は受付処理の外の状態（タイマーが core/Lifecycle.h から渡す）。startup は
	// ツールに ToolContext で渡す。
	ServeResult serve(std::span<const Tool> tools, std::int64_t nowNanos,
	                  Activity activity = Activity::Idle, Startup startup = {});

	bool inSession() const { return !session_.empty(); }

private:
	struct Outcome {
		// 書き出した応答。
		std::string body;
		bool quit = false;
		// 応答を書き込めたら設定する、新しい占有のセッション ID（空の文字列なら占有を終了する）。
		// 無ければ変更しない。
		std::optional<std::string> session;
	};

	// answer は状態（占有のセッション ID・終了の依頼）を変更しない。反映するのは serve で、応答を
	// 書き込めた場合のみ（書き込めなければ呼び出し元は結果を受け取れないのに、占有のみが残存する・
	// 消失する、Vectorworks が終了する）。
	Outcome answer(const std::string &text, std::span<const Tool> tools, std::int64_t nowNanos,
	               Activity activity, Startup startup);

	// held は要求を取り出し、呼び出し元が待機していれば内容を返す（通信規則「待機標識」）。待機する
	// 者が存在しなければ待機標識を削除し、待機標識が無ければ no_wait で応答して、いずれも空を返す。
	std::optional<std::string> held(const std::string &id);

	// refuseAll は受付処理の入れ子の回。取り出せる要求を全て busy で拒否する。解析もツールの実行も
	// 行わない（外の回の占有のセッション ID と競合しない。ツールの SDK 呼び出しの途中で SDK を
	// 呼び出さない）。
	void refuseAll();

	Bridge bridge_;
	std::string session_;
	// serve の途中か（受付処理の入れ子の判定）。
	bool serving_ = false;
	// answer が例外を送出した場合の応答。事前に書き出しておき、catch の中では確保しない（メモリが
	// 不足した場合に再度例外を送出して、応答できなくならないように）。
	std::string internalFailure_;
	// 受付処理の入れ子の回が拒否する応答。事前に書き出しておき、入れ子の回では書き込むのみとする。
	std::string nestedRefusal_;
};

// kindName は @tools の結果に含める種類の綴り。
std::string_view kindName(ToolKind kind);

}
