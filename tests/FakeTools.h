//
//	FakeTools.h
//
//	偽の道具の表。受け付けの手順（core::Server）を SDK なしで確かめるためのもの。
//	作法の見本（protocol/fixtures/serve/）も同じ道具を前提にしていて、Go の偽のプラグインの
//	テスト（cli/internal/fakeplugin/fixtures_test.go）も同じ振る舞いの道具を持つ。
//
//	| 道具   | 種類 | 振る舞い                                   |
//	| ------ | ---- | ------------------------------------------ |
//	| ping   | read | {"pong":true}                              |
//	| echo   | read | {"args":<受け取った引数>}                  |
//	| fail   | read | invalid_args で失敗する                    |
//	| boom   | read | 例外を投げる（internal になる）            |
//	| quit   | app  | {"quitting":true}（受け付けが終了を頼む）  |
//

#pragma once

#include "core/Json.h"
#include "core/Serve.h"

#include <stdexcept>

namespace vwtest {
inline VwCli::core::ToolTable FakeTools() {
	using VwCli::core::Json;
	using VwCli::core::Tool;
	using VwCli::core::ToolContext;
	using VwCli::core::ToolError;
	using VwCli::core::ToolKind;

	const char* const kNoArgs =
	  R"({"type":"object","properties":{},"additionalProperties":false})";
	return {
	  Tool{"ping", "応える", kNoArgs, ToolKind::Read,
	    [](const Json&, const ToolContext&, ToolError&) {
		    Json result = Json::object();
		    result.set("pong", Json::boolean(true));
		    return result;
	    }},
	  Tool{"echo", "引数を返す", R"({"type":"object"})", ToolKind::Read,
	    [](const Json& args, const ToolContext&, ToolError&) {
		    Json result = Json::object();
		    result.set("args", args);
		    return result;
	    }},
	  Tool{"fail", "引数の誤りで失敗する", kNoArgs, ToolKind::Read,
	    [](const Json&, const ToolContext&, ToolError& error) { return error.invalidArgs("引数が誤っています"); }},
	  Tool{"boom", "例外を投げる", kNoArgs, ToolKind::Read,
	    [](const Json&, const ToolContext&, ToolError&) -> Json { throw std::runtime_error("boom"); }},
	  Tool{"quit", "終わる", kNoArgs, ToolKind::App,
	    [](const Json&, const ToolContext&, ToolError&) {
		    Json result = Json::object();
		    result.set("quitting", Json::boolean(true));
		    return result;
	    }},
	};
}
} // namespace vwtest
