#include "TestSupport.h"

#include "core/Ping.h"
#include "core/Protocol.h"

#include <optional>
#include <string>
#include <string_view>

// 道具 ping のうち SDK に依らない部分（docs/plugin/tools.md「ping」）。

using VwCli::core::ErrorCode;
using VwCli::core::Json;
using VwCli::core::PingFacts;
using VwCli::core::ToolContext;
using VwCli::core::ToolError;

namespace {

Json must(std::string_view text) {
	std::optional<Json> value = Json::parse(text, VwCli::core::protocol::kMaxNestingDepth);
	if (!value) {
		VwCli::test::fail(__FILE__, __LINE__, "should parse: " + std::string(text));
		return Json();
	}
	return *value;
}

// pingText は ping の結果のテキスト。protocol は通信規則の版から組み立てる（版を上げても
// ping を変えていなければ落ちないように）。rest は pid の後ろ（session 以降）。
std::string pingText(std::string_view rest) {
	return R"({"version":"abc1234","branch":"main","protocol":)" +
	       std::to_string(VwCli::core::protocol::kVersion) + R"(,"pid":4242,)" + std::string(rest);
}

PingFacts facts(std::optional<std::string> layer) {
	return PingFacts{"abc1234", "main", 4242, std::move(layer)};
}

}

TEST(PingReportsTheCurrentLayer) {
	ToolError error;
	Json result = VwCli::core::ping(Json::object(), ToolContext{false}, facts("1F"), error);
	CHECK(error.code == ErrorCode::None);
	CHECK_EQ(result,
	         must(pingText(R"("session":false,"document_open":true,"current_layer":"1F"})")));
}

TEST(PingWithoutADocumentHasNoLayer) {
	ToolError error;
	Json result = VwCli::core::ping(Json::object(), ToolContext{true}, facts(std::nullopt), error);
	CHECK(error.code == ErrorCode::None);
	CHECK_EQ(result,
	         must(pingText(R"("session":true,"document_open":false,"current_layer":null})")));
}

// 名前の空のレイヤも図面が開いている（null ではなく空の文字列）。
TEST(PingKeepsAnEmptyLayerName) {
	ToolError error;
	Json result = VwCli::core::ping(Json::object(), ToolContext{false}, facts(""), error);
	REQUIRE(result.isObject());
	CHECK_EQ(*result.find("document_open"), Json::boolean(true));
	CHECK_EQ(*result.find("current_layer"), Json::string(""));
}

// 版の表示は書いた順に並ぶ（docs/plugin/tools.md「ping」の例と同じ順）。
TEST(PingWritesTheFieldsInTheDocumentedOrder) {
	ToolError error;
	Json result = VwCli::core::ping(Json::object(), ToolContext{false}, facts("1F"), error);
	CHECK_EQ(result.dump(),
	         pingText(R"("session":false,"document_open":true,"current_layer":"1F"})"));
}

TEST(PingRejectsUnknownArguments) {
	ToolError error;
	Json result =
	    VwCli::core::ping(must(R"({"verbose":true})"), ToolContext{false}, facts("1F"), error);
	CHECK(error.code == ErrorCode::InvalidArgs);
	CHECK(error.message.find("verbose") != std::string::npos);
	CHECK(result.isNull());
}
