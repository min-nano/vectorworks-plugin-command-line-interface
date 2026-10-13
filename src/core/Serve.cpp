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

Json failure(std::string_view code, std::string message) {
	return Json::object({{"ok", Json::boolean(false)},
	                     {"code", Json::string(std::string(code))},
	                     {"error", Json::string(std::move(message))}});
}

struct Request {
	std::string tool;
	Json args;
	// 空なら占有の印を載せていない（null と空の文字列も省略と同じ）。
	std::string session;
};

// readRequest は要求を読む（作法「要求」）。読めなければ空で、reason に理由を書く。キーは
// 大文字と小文字を区別して照合し、知らないキーは無視する。
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

// newSession は占有の印を作る。綴りは id と同じで、先頭の時刻に推測されにくい乱数を続ける。
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
	for (const Tool &tool : tools) {
		if (name == tool.name) {
			return &tool;
		}
	}
	return nullptr;
}

// toolsList は予約された道具 tools の結果（作法「予約された道具」）。
std::optional<Json> toolsList(std::span<const Tool> tools, std::string &reason) {
	Json list = Json::array();
	for (const Tool &tool : tools) {
		std::optional<Json> schema = Json::parse(tool.inputSchema, protocol::kMaxNestingDepth);
		if (!schema) {
			reason = std::string("道具 ") + tool.name + " の inputSchema が JSON として読めません";
			return std::nullopt;
		}
		list.push(Json::object({{"name", Json::string(tool.name)},
		                        {"description", Json::string(tool.description)},
		                        {"inputSchema", std::move(*schema)},
		                        {"kind", Json::string(std::string(kindName(tool.kind)))}}));
	}
	return Json::object(
	    {{"protocol", Json::integer(protocol::kVersion)}, {"tools", std::move(list)}});
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
      internalFailure_(
          failure(protocol::kCodeInternal, "要求の処理の中で例外が起きました").dump()) {}

ServeResult Server::serve(std::span<const Tool> tools, std::int64_t nowNanos) {
	ServeResult result;
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
	for (const std::string &id : bridge_.poll()) {
		std::optional<std::string> text = bridge_.take(id);
		if (!text) {
			continue;
		}
		// 消した直後、実行する前に、呼ぶ側が待っているかを確かめる（作法「待つ印」）。
		switch (bridge_.waitState(id)) {
		case files::WaitState::Released:
			bridge_.removeWait(id);
			continue;
		case files::WaitState::Missing:
			bridge_.reply(id, failure(protocol::kCodeNoWait,
			                          "待つ印（" + id + ".wait）が無いので実行しませんでした")
			                      .dump());
			continue;
		case files::WaitState::Held:
			break;
		}
		// 要求はもう消したので、ここで投げると誰も応えない（呼ぶ側は「実行されたかもしれない」で
		// 終わる）。道具の外（印の乱数・メモリが足りないなど）で投げた例外も internal で応える。
		// catch の中では確保しない（前もって書き出した応答を指すだけ）。
		Outcome outcome;
		const std::string *body = nullptr;
		try {
			outcome = answer(*text, tools, nowNanos);
			body = &outcome.body;
		} catch (...) {
			// answer は投げるとき状態を変えていない（Serve.h）。outcome も代入されていない。
			body = &internalFailure_;
		}
		bridge_.reply(id, *body);
		if (outcome.quit) {
			result.quit = true;
			break;
		}
	}
	return result;
}

Server::Outcome Server::answer(const std::string &text, std::span<const Tool> tools,
                               std::int64_t nowNanos) {
	auto respond = [](const Json &response, bool quit = false) {
		return Outcome{response.dump(), quit};
	};
	std::string reason;
	std::optional<Request> request = readRequest(text, reason);
	if (!request) {
		return respond(failure(protocol::kCodeInvalidRequest, reason));
	}

	// 占有に照らす（作法「占有」の表）。断るなら応答を返す。
	auto check = [&](bool needSession) -> std::optional<Json> {
		if (session_.empty()) {
			if (!request->session.empty()) {
				return failure(protocol::kCodeNoSession,
				               "載せた印はいまの占有のものではありません（占有は終わったか、"
				               "Vectorworks を起動し直しました）");
			}
			if (needSession) {
				return failure(protocol::kCodeNoSession,
				               request->tool + " は占有の中でだけ呼べます");
			}
			return std::nullopt;
		}
		if (request->session != session_) {
			return failure(protocol::kCodeBusy, "ほかの呼ぶ側が占有しています");
		}
		return std::nullopt;
	};

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
			return respond(failure(protocol::kCodeBusy, "ほかの呼ぶ側が占有しています"));
		}
		if (std::optional<Json> refusal = check(false)) {
			return respond(*refusal);
		}
		std::string session = newSession(nowNanos);
		Outcome outcome = respond(success(Json::object({{"session", Json::string(session)}})));
		session_ = std::move(session);
		return outcome;
	}
	if (name == protocol::kToolSessionEnd) {
		if (std::optional<Json> refusal = check(true)) {
			return respond(*refusal);
		}
		Outcome outcome = respond(success(Json::object({{"ended", Json::boolean(true)}})));
		session_.clear();
		return outcome;
	}

	const Tool *tool = findTool(tools, name);
	// ping は誰にでも答える。種類 App（quit）は占有の中でだけ呼べる。表に無い道具も、占有に
	// 照らしてから知らないと答える。
	if (name != protocol::kToolPing) {
		if (std::optional<Json> refusal = check(tool && tool->kind == ToolKind::App)) {
			return respond(*refusal);
		}
	}
	if (!tool) {
		return respond(failure(protocol::kCodeUnknownTool, "知らない道具です: " + name));
	}

	// 道具 1 つの失敗で橋を止めない。例外はここで受ける。
	ToolError error;
	Json result;
	try {
		result = tool->run(request->args, ToolContext{inSession()}, error);
	} catch (const std::exception &exception) {
		return respond(failure(protocol::kCodeInternal,
		                       std::string("道具の中で例外が起きました: ") + exception.what()));
	} catch (...) {
		return respond(failure(protocol::kCodeInternal, "道具の中で例外が起きました"));
	}
	switch (error.code) {
	case ErrorCode::None:
		break;
	case ErrorCode::InvalidArgs:
		return respond(failure(protocol::kCodeInvalidArgs, error.message));
	case ErrorCode::Internal:
		return respond(failure(protocol::kCodeInternal, error.message));
	}
	// 道具はもう実行したので、ここで投げると、実行した要求に internal で応えることになる（避け
	// られない。応答を書き出すための確保だけが残る）。
	return respond(success(std::move(result)), tool->kind == ToolKind::App);
}

}
