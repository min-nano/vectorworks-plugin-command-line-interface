#include "core/Serve.h"

#include "core/Protocol.h"

#include <algorithm>
#include <cstdio>
#include <exception>
#include <optional>
#include <random>

namespace VwCli::core {

namespace {

Json success(Json result) {
	return Json::object({{"ok", Json::boolean(true)}, {"result", std::move(result)}});
}

// failure は失敗の応答。detail はツールが返した失敗の詳細で、null なら付けない（通信規則
// 「応答」）。
Json failure(std::string_view code, std::string message, Json detail = Json()) {
	Json response = Json::object({{"ok", Json::boolean(false)},
	                              {"code", Json::string(std::string(code))},
	                              {"error", Json::string(std::move(message))}});
	if (!detail.isNull()) {
		response.set("detail", std::move(detail));
	}
	return response;
}

// staleSession は、付与されたセッション ID が現在の占有のものでない場合の応答。
Json staleSession() {
	return failure(protocol::kCodeNoSession,
	               "載せた印はいまの占有のものではありません（占有は終わったか、"
	               "Vectorworks を起動し直しました）");
}

// occupied は、他の呼び出し元が占有している場合の応答。
Json occupied() { return failure(protocol::kCodeOccupied, "ほかの呼ぶ側が占有しています"); }

// toolDefect は、ツールの定義か返した値が通信規則に適合しない場合の文言
// （ツールの不具合。internal で応答する）。
std::string toolDefect(std::string_view name, std::string_view what) {
	return "道具 " + std::string(name) + " の " + std::string(what);
}

struct Request {
	std::string tool;
	Json args;
	// 空なら占有のセッション ID を付与していない（null と空の文字列も省略と同様）。
	std::string session;
};

// readRequest は要求を読み取る（通信規則「要求」）。読み取れなければ空で、reason に理由を書き込む。
// キーは大文字と小文字を区別して照合し、未知のキーは無視する。
std::optional<Request> readRequest(const std::string &text, std::string &reason) {
	if (text.size() > protocol::kMaxRequestBytes) {
		reason =
		    "要求が大きすぎます（上限 " + std::to_string(protocol::kMaxRequestBytes) + " バイト）";
		return std::nullopt;
	}
	std::string error;
	std::optional<Json> json = Json::parse(text, protocol::kMaxNestingDepth, &error);
	if (!json) {
		reason = "要求を JSON として読めません（" + error + "）";
		return std::nullopt;
	}
	if (!json->isObject()) {
		reason = "要求がオブジェクトではありません";
		return std::nullopt;
	}
	Request request;
	const Json *tool = json->find("tool");
	if (!tool || !tool->isString() || tool->asString().empty()) {
		reason = "tool が空でない文字列ではありません";
		return std::nullopt;
	}
	request.tool = tool->asString();
	const Json *args = json->find("args");
	if (!args || args->isNull()) {
		request.args = Json::object();
	} else if (args->isObject()) {
		request.args = *args;
	} else {
		reason = "args がオブジェクトではありません";
		return std::nullopt;
	}
	if (const Json *session = json->find("session"); session && !session->isNull()) {
		if (!session->isString()) {
			reason = "session が文字列ではありません";
			return std::nullopt;
		}
		request.session = session->asString();
	}
	return request;
}

// newSession は占有のセッション ID を生成する。綴りは id と同じで、先頭の時刻に推測されにくい
// 乱数を続ける。
std::string newSession(std::int64_t nowNanos) {
	std::random_device random;
	std::uint64_t bits = (static_cast<std::uint64_t>(random()) << 32) | random();
	char text[64];
	std::snprintf(text, sizeof text, "%019lld-%016llx",
	              static_cast<long long>(std::max<std::int64_t>(nowNanos, 0)),
	              static_cast<unsigned long long>(bits));
	return text;
}

const Tool *findTool(std::span<const Tool> tools, const std::string &name) {
	auto found = std::ranges::find(tools, std::string_view(name), &Tool::name);
	return found == tools.end() ? nullptr : &*found;
}

// toolsList は予約ツール @tools の結果（通信規則「予約ツール」）。
std::optional<Json> toolsList(std::span<const Tool> tools, std::string &reason) {
	Json list = Json::array();
	for (const Tool &tool : tools) {
		std::optional<Json> schema = Json::parse(tool.inputSchema, protocol::kMaxNestingDepth);
		if (!schema) {
			reason = toolDefect(tool.name, "inputSchema が JSON として読めません");
			return std::nullopt;
		}
		list.push(Json::object({{"name", Json::string(std::string(tool.name))},
		                        {"description", Json::string(tool.description)},
		                        {"inputSchema", std::move(*schema)},
		                        {"kind", Json::string(std::string(kindName(tool.kind)))}}));
	}
	return Json::object(
	    {{"protocol", Json::integer(protocol::kVersion)}, {"tools", std::move(list)}});
}

// refuseOutsideSession は、占有の中でのみ呼び出せる要求（@session_end・種類 Write と App）を、
// 現在の占有 current と照合する（通信規則「占有」の表）。拒否する場合は応答を返す。
std::optional<Json> refuseOutsideSession(const std::string &current, const Request &request) {
	if (current.empty()) {
		if (!request.session.empty()) {
			return staleSession();
		}
		return failure(protocol::kCodeNoSession, request.tool + " は占有の中でだけ呼べます");
	}
	if (request.session != current) {
		return occupied();
	}
	return std::nullopt;
}

// ToolReply はツールを実行した結果。応答と、終了の依頼（ツールが成功して設定した場合のみ真）。
struct ToolReply {
	Json response;
	bool quit = false;
};

// runTool はツールを実行して応答を返す（通信規則「応答」）。1 つのツールの失敗でブリッジを停止
// させないよう、例外はここで捕捉する。
ToolReply runTool(const Tool &tool, const Json &args, const ToolContext &context) {
	ToolStatus status;
	Json result;
	try {
		result = tool.run(args, context, status);
	} catch (const std::exception &exception) {
		return {failure(protocol::kCodeInternal,
		                std::string("道具の中で例外が起きました: ") + exception.what())};
	} catch (...) {
		return {failure(protocol::kCodeInternal, "道具の中で例外が起きました")};
	}
	// 終了の依頼は、ツールが成功した場合のみ発行する（失敗した quit は終了を依頼しない）。
	// 種類では決定しない。
	if (status.code != ErrorCode::None) {
		// detail は JSON のオブジェクト（通信規則「応答」）。オブジェクトでない値は
		// ツールの不具合として internal で応答し、detail を付けない。
		if (!status.detail.isNull() && !status.detail.isObject()) {
			return {failure(protocol::kCodeInternal,
			                toolDefect(tool.name, "detail がオブジェクトではありません"))};
		}
		std::string_view code = status.code == ErrorCode::InvalidArgs ? protocol::kCodeInvalidArgs
		                                                              : protocol::kCodeInternal;
		return {failure(code, std::move(status.message), std::move(status.detail))};
	}
	// result は必ずオブジェクト（通信規則「応答」）。値を返さないツールは {} にする。
	// オブジェクトでない値はツールの不具合として internal で応答し、失敗であるため終了を依頼しない
	// （失敗の終了コードを受け取った呼び出し元が、終了しつつある Vectorworks に対して動作しない
	// ように）。
	if (result.isNull()) {
		result = Json::object();
	} else if (!result.isObject()) {
		return {failure(protocol::kCodeInternal,
		                toolDefect(tool.name, "結果がオブジェクトではありません"))};
	}
	return {success(std::move(result)), status.quit};
}

}

std::string_view kindName(ToolKind kind) {
	switch (kind) {
	case ToolKind::Read:
		return protocol::kKindRead;
	case ToolKind::Write:
		return protocol::kKindWrite;
	case ToolKind::App:
		return protocol::kKindApp;
	}
	return protocol::kKindWrite;
}

Server::Server(std::string spoolDir)
    : bridge_(std::move(spoolDir)),
      internalFailure_(failure(protocol::kCodeInternal, "要求の処理の中で例外が起きました").dump()),
      nestedRefusal_(
          failure(protocol::kCodeBusy, "ほかの要求を処理している最中なので実行しませんでした")
              .dump()) {}

ServeResult Server::serve(std::span<const Tool> tools, std::int64_t nowNanos, Activity activity,
                          Startup startup) {
	ServeResult result;
	if (serving_) {
		refuseAll();
		return result;
	}
	if (!bridge_.locked()) {
		if (!bridge_.prepare(result.message)) {
			result.phase = Phase::Error;
			return result;
		}
		switch (bridge_.lock(result.message)) {
		case files::Lock::Result::Taken:
			bridge_.sweep();
			break;
		case files::Lock::Result::Busy:
			result.phase = Phase::Waiting;
			return result;
		case files::Lock::Result::Failed:
			result.phase = Phase::Error;
			return result;
		}
	}
	// ツールの中で Vectorworks がイベントを処理すると、この回の途中で再度 serve が呼び出されうる。
	// 戻る際（例外で抜ける場合も）に解除する。
	ScopedFlag serving(serving_);
	for (const std::string &id : bridge_.poll()) {
		std::optional<std::string> text = held(id);
		if (!text) {
			continue;
		}
		// 要求は既に削除したため、ここで例外が送出されると誰も応答しない（呼び出し元は
		// 「実行されたかもしれない」で終了する）。ツールの外（セッション ID の乱数・メモリの不足
		// など）で送出された例外も internal で応答する。catch の中では確保しない（事前に書き出した
		// 応答を指すのみ）。応答を書き込む reply はパスを構築するため確保する。そこでも
		// 確保できなければ、例外は serve の外（Clock）へ送出され、この要求には応答しない。
		Outcome outcome;
		const std::string *body = nullptr;
		try {
			outcome = answer(*text, tools, nowNanos, activity, startup);
			body = &outcome.body;
		} catch (...) {
			// answer は状態を変更しない（Serve.h）。outcome も代入されていない。
			body = &internalFailure_;
		}
		// 占有のセッション ID と終了の依頼は、応答を書き込めた場合のみ反映する（Serve.h。通信規則
		// 「占有」）。
		if (!bridge_.reply(id, *body)) {
			continue;
		}
		if (outcome.session) {
			session_ = std::move(*outcome.session);
		}
		if (outcome.quit) {
			result.quit = true;
			break;
		}
	}
	return result;
}

std::optional<std::string> Server::held(const std::string &id) {
	std::optional<std::string> text = bridge_.take(id);
	if (!text) {
		return std::nullopt;
	}
	// 削除した直後、実行する前に、呼び出し元が待機しているかを確認する（通信規則「待機標識」）。
	switch (bridge_.waitState(id)) {
	case files::WaitState::Released:
		bridge_.removeWait(id);
		return std::nullopt;
	case files::WaitState::Missing:
		bridge_.reply(id, failure(protocol::kCodeNoWait,
		                          "待つ印（" + id + ".wait）が無いので実行しませんでした")
		                      .dump());
		return std::nullopt;
	case files::WaitState::Held:
		break;
	}
	return text;
}

void Server::refuseAll() {
	// 入れ子の回は、ツールの SDK 呼び出しが処理するイベントの中で実行される。例外を外へ送出すると
	// SDK と Vectorworks のフレームを越えて巻き戻るため、ここで捕捉する。
	try {
		for (const std::string &id : bridge_.poll()) {
			if (held(id)) {
				bridge_.reply(id, nestedRefusal_);
			}
		}
	} catch (...) {
		// 取り出す前の要求は、外の回か次の周期処理が扱う。取り出した後に例外が送出されたら（応答を
		// 書き込むパスを構築できないなど）その要求には応答せず、呼び出し元は待機の上限に達して
		// 終了する（実行はしていない）。
		return;
	}
}

Server::Outcome Server::answer(const std::string &text, std::span<const Tool> tools,
                               std::int64_t nowNanos, Activity activity, Startup startup) {
	auto respond = [](const Json &response, bool quit = false) {
		Outcome outcome;
		outcome.body = response.dump();
		outcome.quit = quit;
		return outcome;
	};
	std::string reason;
	std::optional<Request> request = readRequest(text, reason);
	if (!request) {
		return respond(failure(protocol::kCodeInvalidRequest, reason));
	}

	const std::string &name = request->tool;
	if (name == protocol::kToolTools) {
		std::optional<Json> list = toolsList(tools, reason);
		if (!list) {
			return respond(failure(protocol::kCodeInternal, reason));
		}
		return respond(success(std::move(*list)));
	}
	if (name == protocol::kToolSessionStart) {
		if (!session_.empty()) {
			return respond(occupied());
		}
		if (!request->session.empty()) {
			return respond(staleSession());
		}
		std::string session = newSession(nowNanos);
		Outcome outcome = respond(success(Json::object({{"session", Json::string(session)}})));
		outcome.session = std::move(session);
		return outcome;
	}
	if (name == protocol::kToolSessionEnd) {
		if (std::optional<Json> refusal = refuseOutsideSession(session_, *request)) {
			return respond(*refusal);
		}
		Outcome outcome = respond(success(Json::object({{"ended", Json::boolean(true)}})));
		outcome.session = std::string();
		return outcome;
	}

	// 一覧に無いツールと種類 Read のツールは占有と照合しない（図面を変更しないため、占有している
	// 呼び出し元の作業と混在しない）。
	const Tool *tool = findTool(tools, name);
	if (tool && tool->kind != ToolKind::Read) {
		if (std::optional<Json> refusal = refuseOutsideSession(session_, *request)) {
			return respond(*refusal);
		}
	}
	// 一覧に無いツールは、終了の依頼の処理中も unknown_tool で応答する（SDK を呼び出さない。綴りの
	// 誤りを、呼び出し直せば通過するかのような busy で返さない）。
	if (!tool) {
		return respond(failure(protocol::kCodeUnknownTool, "知らない道具です: " + name));
	}
	// 終了の依頼の処理中は、一覧のツールを全て拒否する（種類 Read と ping も）。終了の呼び出しの
	// 途中で、ツールに SDK を呼び出させないため。SDK を呼び出さない 3 つのツール（上の @tools・
	// @session_start・@session_end）は応答する。占有との照合が先。
	if (activity == Activity::Quitting) {
		return respond(failure(protocol::kCodeBusy,
		                       "Vectorworks に終了を頼んでいる最中なので実行しませんでした"));
	}

	ToolReply reply = runTool(*tool, request->args, ToolContext{inSession(), startup});
	// ツールは既に実行したため、ここで例外が送出されると、実行した要求に internal で応答することに
	// なる（回避できない。応答を書き出すための確保のみが残る）。
	return respond(reply.response, reply.quit);
}

}
