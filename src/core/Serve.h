#pragma once

// 受け付け 1 回の手順と、占有の印（docs/plugin/bridge.md「受け付け 1 回」）。SDK に依らない。
// 道具の表と時刻を引数で受けるので、偽の道具の表でテストできる。

#include "core/Bridge.h"
#include "core/Json.h"

#include <cstdint>
#include <optional>
#include <span>
#include <string>

namespace VwCli::core {

// 道具の種類（tools の結果の kind）。図面を変えるかが分からない道具は Write とする。Write と App は
// 占有の中でだけ呼べ、Vectorworks が処理中なら busy で断る。Read は占有に照らさない。
enum class ToolKind { Read, Write, App };

// 受け付けの外の状態。タイマー（Clock）が回ごとに渡す。Idle 以外では、種類 Write と App の道具を
// busy で断る（ほかの道具は応える）。
enum class Activity {
	Idle,
	// undo の記録が開いている（IsCurrentlyBuildingAnUndoEvent）。書くと開いている記録に混ざる。
	UndoOpen,
	// 終了の依頼の最中（保存の確認を開いている）。
	Quitting,
};

// 道具が返す失敗の種別。None は失敗していない。
enum class ErrorCode { None, InvalidArgs, Internal };

struct ToolError {
	ErrorCode code = ErrorCode::None;
	// 日本語。応答の error にそのまま載る。
	std::string message;
};

// 道具に渡す受け付けの状態。
struct ToolContext {
	// 占有の印を発行しているか（ping の session）。
	bool session = false;
};

// 道具の中身。失敗は error に書き（戻り値は使わない）、例外は Server が internal で応える。
using ToolFn = Json (*)(const Json &args, const ToolContext &context, ToolError &error);

struct Tool {
	// [a-z0-9_]+(\.[a-z0-9_]+)*
	const char *name;
	// 日本語。呼ぶ側がそのまま読む。
	const char *description;
	// JSON Schema（文字列）。tools の結果にそのまま載る。
	const char *inputSchema;
	ToolKind kind;
	ToolFn run;
};

// 受け付けの状態。
enum class Phase {
	// ロックを掴んで受け付けた
	Serving,
	// ほかの Vectorworks がロックを掴んでいるので、何もしなかった
	Waiting,
	// スプールを用意できない・ロックのファイルを開けない（次の回にやり直す）
	Error,
};

struct ServeResult {
	Phase phase = Phase::Serving;
	// Error の理由。
	std::string message;
	// 種類 App の道具が成功し、その応答を書けた（終了の依頼）。受け付けの外で終了を頼む。
	bool quit = false;
};

// Server はスプール 1 つの受け付けと、発行した占有の印（1 つだけ。メモリだけに持つ）。
class Server {
public:
	explicit Server(std::string spoolDir);

	// serve は受け付け 1 回。nowNanos は Unix 時刻のナノ秒で、占有の印の先頭に使う。
	//
	// ロックを掴んでいなければ用意（prepare → lock → sweep）をし、取れなければ何もせずに戻る。
	// 回の始めに並べた要求を名前の昇順ですべて処理し、取り出した要求には同じ回の中で応える。
	// 種類 App の道具が成功して応答を書けたら、その回の残りの要求を取り出さずに戻る（終了したら
	// 応えられないので、残りは置いたままにする）。
	//
	// 道具の中で Vectorworks がイベントを回し、serve の途中でまた serve が呼ばれたら（受け付けの
	// 入れ子）、外の回の状態に触れず、取り出せる要求をすべて busy で断って戻る。
	ServeResult serve(std::span<const Tool> tools, std::int64_t nowNanos,
	                  Activity activity = Activity::Idle);

	bool inSession() const { return !session_.empty(); }

private:
	struct Outcome {
		// 書き出した応答。
		std::string body;
		bool quit = false;
		// 応答を書けたら置く、新しい占有の印（空の文字列なら占有を終える）。無ければ変えない。
		std::optional<std::string> session;
	};

	// answer は状態（占有の印・終了の依頼）を変えない。反映するのは serve で、応答を書けたときだけ
	// （書けないと呼ぶ側は結果を受け取れないのに、占有だけが残る・消える、Vectorworks が終わる）。
	Outcome answer(const std::string &text, std::span<const Tool> tools, std::int64_t nowNanos,
	               Activity activity);

	// held は要求を取り出し、呼ぶ側が待っていれば中身を返す（通信規則「待つ印」）。待つ者が
	// 居なければ待つ印を消し、待つ印が無ければ no_wait で応えて、どちらも空を返す。
	std::optional<std::string> held(const std::string &id);

	// refuseAll は受け付けの入れ子の回。取り出せる要求をすべて busy で断る。解析も道具の実行も
	// しない（外の回の占有の印と取り合わない。道具の SDK 呼び出しの途中で SDK を呼ばない）。
	void refuseAll();

	Bridge bridge_;
	std::string session_;
	// serve の途中か（受け付けの入れ子の判定）。
	bool serving_ = false;
	// answer が投げたときの応答。前もって書き出しておき、catch の中では確保しない（メモリが足りない
	// ときにまた投げて、応えられなくならないように）。
	std::string internalFailure_;
};

// kindName は tools の結果に載せる種類の綴り。
std::string_view kindName(ToolKind kind);

}
