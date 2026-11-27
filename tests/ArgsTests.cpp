#include "TestSupport.h"

#include "core/Args.h"
#include "core/Json.h"
#include "core/Serve.h"

#include <cstddef>
#include <optional>
#include <string>
#include <vector>

// 道具に共通の手順（core/Args.h）のうち、道具のテストからは文言まで見ていないもの。findOne の
// 失敗の文言は、呼ぶ側（findTarget・findLayer）と組み立てを分けているので、ここで綴りを確かめる。

using VwCli::core::ErrorCode;
using VwCli::core::Json;
using VwCli::core::ToolStatus;
using VwCli::test::must;

namespace {

// Calls は findOne が文言の関数を呼んだ回数（失敗するときだけ組み立てることを確かめる）。
struct Calls {
	int missing = 0;
	int ambiguous = 0;
};

std::optional<std::size_t> findName(const std::vector<std::string> &names, const std::string &name,
                                    Calls &calls, ToolStatus &status) {
	return VwCli::core::args::findOne(
	    names, [&](const std::string &candidate) { return candidate == name; }, "tool_x",
	    [&] {
		    ++calls.missing;
		    return name + " がありません";
	    },
	    "x_ambiguous",
	    [&](int matches) {
		    ++calls.ambiguous;
		    return name + " が " + std::to_string(matches) + " つあります";
	    },
	    status);
}

}

TEST(FindOneReturnsThePositionOfTheOnlyMatch) {
	Calls calls;
	ToolStatus status;
	std::optional<std::size_t> at = findName({"a", "b", "c"}, "b", calls, status);
	CHECK(at.has_value());
	CHECK_EQ(at.value_or(0), std::size_t{1});
	CHECK(status.code == ErrorCode::None);
	CHECK_EQ(status.message, std::string());
	CHECK(status.detail.isNull());
	// 成功するときは文言を組み立てない。
	CHECK_EQ(calls.missing, 0);
	CHECK_EQ(calls.ambiguous, 0);
}

TEST(FindOneRefusesNoMatchAsInvalidArgs) {
	Calls calls;
	ToolStatus status;
	std::optional<std::size_t> at = findName({"a", "b"}, "z", calls, status);
	CHECK(!at.has_value());
	CHECK(status.code == ErrorCode::InvalidArgs);
	CHECK_EQ(status.message, std::string("tool_x の引数が誤っています: z がありません"));
	CHECK(status.detail.isNull());
	CHECK_EQ(calls.missing, 1);
	CHECK_EQ(calls.ambiguous, 0);
}

TEST(FindOneRefusesAnEmptyListAsInvalidArgs) {
	Calls calls;
	ToolStatus status;
	CHECK(!findName({}, "a", calls, status).has_value());
	CHECK(status.code == ErrorCode::InvalidArgs);
	CHECK_EQ(status.message, std::string("tool_x の引数が誤っています: a がありません"));
}

TEST(FindOneRefusesTwoMatchesWithTheReason) {
	Calls calls;
	ToolStatus status;
	std::optional<std::size_t> at = findName({"a", "b", "a"}, "a", calls, status);
	CHECK(!at.has_value());
	CHECK(status.code == ErrorCode::Internal);
	CHECK_EQ(status.message, std::string("a が 2 つあります"));
	CHECK_EQ(status.detail, must(R"({"reason":"x_ambiguous"})"));
	CHECK_EQ(calls.missing, 0);
	CHECK_EQ(calls.ambiguous, 1);
}

TEST(FindOneCountsEveryMatch) {
	Calls calls;
	ToolStatus status;
	CHECK(!findName({"a", "a", "a"}, "a", calls, status).has_value());
	CHECK_EQ(status.message, std::string("a が 3 つあります"));
}

TEST(ListResultPutsTheListAndItsCount) {
	Json list = Json::array();
	list.push(Json::string("x"));
	list.push(Json::string("y"));
	CHECK_EQ(VwCli::core::args::listResult("names", std::move(list)),
	         must(R"({"names":["x","y"],"count":2})"));
	CHECK_EQ(VwCli::core::args::listResult("names", Json::array()),
	         must(R"({"names":[],"count":0})"));
}
