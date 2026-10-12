//
// core/Serve.cpp
//
// 受け付け 1 回の手順の実装（意図と制約は core/Serve.h）。
//

#include "core/Serve.h"
#include "core/Bridge.h"
#include "core/Json.h"

#include <algorithm>
#include <cstddef>
#include <exception>
#include <filesystem>
#include <random>
#include <set>
#include <string>
#include <utility>

namespace VwCli::core {
namespace {
constexpr const char* kToolTools = "tools";
constexpr const char* kToolSessionStart = "session_start";
constexpr const char* kToolSessionEnd = "session_end";
constexpr const char* kToolPing = "ping";

const Tool* FindTool(const ToolTable& tools, const std::string& name) {
	const auto found = std::find_if(
		tools.begin(), tools.end(), [&name](const Tool& tool) { return tool.name == name; });
	return found != tools.end() ? &*found : nullptr;
}

// tools の結果（docs/protocol.md「予約された道具」）。予約された道具（tools・
// session_start・session_end）は載せない。
Json ListTools(const ToolTable& tools) {
	Json list = Json::array();
	for (const Tool& tool : tools) {
		Json schema;
		std::string error;
		if (!Json::parse(tool.inputSchema, schema, error) || !schema.isObject())
			schema = Json::object();
		Json item = Json::object();
		item.set("name", Json::string(tool.name));
		item.set("description", Json::string(tool.description));
		item.set("inputSchema", std::move(schema));
		item.set("kind", Json::string(toolKindName(tool.kind)));
		list.push(std::move(item));
	}
	Json result = Json::object();
	result.set("protocol", Json::integer(kBridgeProtocolVersion));
	result.set("tools", std::move(list));
	return result;
}

bool IsNameChar(char ch) {
	return (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '_';
}
} // namespace

// -----------------------------------------------------------------------
const char* toolKindName(ToolKind kind) {
	switch (kind) {
	case ToolKind::Read:
		return "read";
	case ToolKind::Write:
		return "write";
	case ToolKind::App:
		return "app";
	}
	return "write";
}

Json ToolError::invalidArgs(std::string why) {
	failed = true;
	code = BridgeCode::InvalidArgs;
	message = std::move(why);
	return Json::null();
}

Json ToolError::internal(std::string why) {
	failed = true;
	code = BridgeCode::Internal;
	message = std::move(why);
	return Json::null();
}

bool isValidToolName(const std::string& name) {
	// '.' で区切った各部が [a-z0-9_]+。
	bool partEmpty = true;
	for (const char ch : name) {
		if (ch == '.') {
			if (partEmpty)
				return false;
			partEmpty = true;
			continue;
		}
		if (!IsNameChar(ch))
			return false;
		partEmpty = false;
	}
	return !partEmpty;
}

bool isBuiltinToolName(const std::string& name) {
	return name == kToolTools || name == kToolSessionStart || name == kToolSessionEnd;
}

bool validateToolTable(const ToolTable& tools, std::string& error) {
	error.clear();
	std::set<std::string> seen;
	for (const Tool& tool : tools) {
		if (!isValidToolName(tool.name))
			error = "道具の名前の綴りが正しくありません: " + tool.name;
		else if (isBuiltinToolName(tool.name))
			error = "予約された道具は表に載せません: " + tool.name;
		else if (!seen.insert(tool.name).second)
			error = "道具の名前が重なっています: " + tool.name;
		else if (tool.description.empty())
			error = "道具の説明がありません: " + tool.name;
		else if (!tool.run)
			error = "道具の中身がありません: " + tool.name;
		else if (tool.name == kToolPing && tool.kind != ToolKind::Read)
			error = "ping は種類 read でなければなりません";
		else {
			Json schema;
			std::string why;
			if (!Json::parse(tool.inputSchema, schema, why) || !schema.isObject())
				error = "inputSchema がオブジェクトの JSON ではありません: " + tool.name;
		}
		if (!error.empty())
			return false;
	}
	return true;
}

std::string newSessionId() {
	// random_device は OS の乱数（Windows の rand_s・mac の arc4random・Linux の
	// /dev/urandom など）から取る。128 ビットを 16 進 32 桁にする。
	std::random_device device;
	static const char* const kHex = "0123456789abcdef";
	std::string id;
	for (int i = 0; i < 4; ++i) {
		const auto word = static_cast<unsigned int>(device());
		for (int shift = 28; shift >= 0; shift -= 4)
			id += kHex[(word >> static_cast<unsigned int>(shift)) & 0x0FU];
	}
	return id;
}

// -----------------------------------------------------------------------
Server::Server(std::filesystem::path spoolDir) : fSpool(std::move(spoolDir)) {}

ServeResult Server::serve(const ToolTable& tools) {
	ServeResult result;

	// 1. ロックを掴んでいなければ用意して掴み、取れたら 1 度だけ残骸を消す。
	if (!fSpool.locked()) {
		std::string error;
		if (!fSpool.prepare(error)) {
			result.phase = ServePhase::Error;
			result.message = error;
			return result;
		}
		switch (fSpool.lock(error)) {
		case LockResult::Locked:
			break;
		case LockResult::HeldElsewhere:
			result.phase = ServePhase::Waiting;
			return result;
		case LockResult::Failed:
			result.phase = ServePhase::Error;
			result.message = error;
			return result;
		}
		fSpool.sweep();
	}
	result.phase = ServePhase::Serving;

	// 2. 並べるのは回の始めに 1 度だけ。その後に置かれた要求は次の回に回す。
	for (const std::string& id : fSpool.poll()) {
		std::string text;
		const TakeResult taken = fSpool.take(id, text);
		if (taken == TakeResult::Skipped)
			continue;

		// 消した直後、実行する前に待つ印を確かめる。
		BridgeResponse response;
		bool ranApp = false;
		switch (fSpool.waitState(id)) {
		case WaitState::Released:
			// 呼ぶ側は居ない。実行も応答もしない。
			fSpool.removeWait(id);
			continue;
		case WaitState::Missing:
			response = BridgeResponse::failure(BridgeCode::NoWait,
				"待つ印（" + id + kBridgeWaitSuffix + "）が無いので実行しませんでした");
			break;
		case WaitState::Held: {
			// 3. 読めない要求には invalid_request で応える。
			BridgeRequest request;
			std::string error;
			if (taken == TakeResult::TooLarge)
				response = BridgeResponse::failure(BridgeCode::InvalidRequest,
					"要求が大きすぎます（上限 " + std::to_string(kBridgeMaxRequestBytes) +
						" バイト）");
			else if (!parseBridgeRequest(text, request, error))
				response = BridgeResponse::failure(
					BridgeCode::InvalidRequest, "要求が読めません: " + error);
			else
				response = answerSafely(request, tools, ranApp);
			break;
		}
		}

		std::string error;
		if (!fSpool.reply(id, response, error))
			result.message = error;

		// 4. 種類 app の道具は応えてから終了を頼み、残りの要求は置いたままにする（終了
		// したら応えられないため）。
		if (ranApp) {
			result.quit = true;
			return result;
		}
	}
	return result;
}

bool Server::admit(const std::string& session, bool needSession, BridgeResponse& refusal) const {
	if (fSession.empty()) {
		if (!session.empty()) {
			refusal = BridgeResponse::failure(
				BridgeCode::NoSession, "その占有の印はいまの占有のものではありません");
			return false;
		}
		if (needSession) {
			refusal =
				BridgeResponse::failure(BridgeCode::NoSession, "この道具は占有の中でだけ呼べます");
			return false;
		}
		return true;
	}
	if (session != fSession) {
		refusal = BridgeResponse::failure(BridgeCode::Busy, "ほかの呼ぶ側が占有しています");
		return false;
	}
	return true;
}

BridgeResponse Server::answerSafely(
	const BridgeRequest& request, const ToolTable& tools, bool& ranApp) {
	// 道具の外（表の引き・結果の組み立て）で起きた例外も、その要求の失敗にとどめる。
	try {
		return answer(request, tools, ranApp);
	} catch (const std::exception& e) {
		ranApp = false;
		return BridgeResponse::failure(
			BridgeCode::Internal, std::string("受け付けの中で例外が起きました: ") + e.what());
	} catch (...) {
		ranApp = false;
		return BridgeResponse::failure(BridgeCode::Internal, "受け付けの中で例外が起きました");
	}
}

BridgeResponse Server::answer(const BridgeRequest& request, const ToolTable& tools, bool& ranApp) {
	ranApp = false;
	BridgeResponse refusal;
	const std::string& name = request.tool;

	// 予約された道具は表を検索しない。
	if (name == kToolTools)
		return BridgeResponse::success(ListTools(tools));
	if (name == kToolSessionStart) {
		if (!fSession.empty())
			return BridgeResponse::failure(BridgeCode::Busy, "ほかの呼ぶ側が占有しています");
		if (!admit(request.session, false, refusal))
			return refusal;
		std::string issued;
		try {
			issued = newSessionId();
		} catch (const std::exception& e) {
			return BridgeResponse::failure(
				BridgeCode::Internal, std::string("占有の印を作れません: ") + e.what());
		}
		fSession = issued;
		Json result = Json::object();
		result.set("session", Json::string(issued));
		return BridgeResponse::success(std::move(result));
	}
	if (name == kToolSessionEnd) {
		if (!admit(request.session, true, refusal))
			return refusal;
		fSession.clear();
		Json result = Json::object();
		result.set("ended", Json::boolean(true));
		return BridgeResponse::success(std::move(result));
	}

	// ping は占有によらず誰にでも答える。そのほかは表を引く前に占有に照らす（知らない
	// 道具も、ほかが占有していれば busy）。
	const Tool* tool = FindTool(tools, name);
	if (name != kToolPing) {
		const bool needSession = tool != nullptr && tool->kind == ToolKind::App;
		if (!admit(request.session, needSession, refusal))
			return refusal;
	}
	if (tool == nullptr)
		return BridgeResponse::failure(BridgeCode::UnknownTool, "知らない道具です: " + name);

	// 道具 1 つの失敗（例外を含む）で橋を止めない。
	ToolContext context;
	context.session = !fSession.empty();
	ToolError error;
	Json result;
	try {
		result = tool->run(request.args, context, error);
	} catch (const std::exception& e) {
		return BridgeResponse::failure(
			BridgeCode::Internal, std::string("道具の中で例外が起きました: ") + e.what());
	} catch (...) {
		return BridgeResponse::failure(BridgeCode::Internal, "道具の中で例外が起きました");
	}
	if (error.failed) {
		// 道具が付けてよい種別は invalid_args と internal だけ。
		const BridgeCode code =
			error.code == BridgeCode::InvalidArgs ? BridgeCode::InvalidArgs : BridgeCode::Internal;
		return BridgeResponse::failure(
			code, error.message.empty() ? "道具が失敗しました" : error.message);
	}
	ranApp = tool->kind == ToolKind::App;
	return BridgeResponse::success(std::move(result));
}
} // namespace VwCli::core
