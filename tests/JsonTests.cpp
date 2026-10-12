#include "TestSupport.h"

#include "core/Json.h"

#include <chrono>
#include <cstdint>
#include <string>

using VwCli::core::Json;

namespace {

constexpr int kDepth = 64;

std::optional<Json> parse(std::string_view text, int depth = kDepth) {
	return Json::parse(text, depth);
}

// must は読めるはずのテキストを読む（値で返すので、式の中で使っても寿命を気にしなくてよい）。
Json must(std::string_view text) {
	std::optional<Json> value = Json::parse(text, kDepth);
	if (!value) {
		VwCli::test::fail(__FILE__, __LINE__, "should parse: " + std::string(text));
		return Json();
	}
	return *value;
}

std::string nested(char open, char close, int depth) {
	return std::string(static_cast<std::size_t>(depth), open) +
	       std::string(static_cast<std::size_t>(depth), close);
}

std::string nestedObjects(int depth) {
	std::string text;
	for (int i = 1; i < depth; ++i) {
		text += "{\"a\":";
	}
	text += "{}";
	text += std::string(static_cast<std::size_t>(depth - 1), '}');
	return text;
}

}

TEST(JsonParsesEveryType) {
	std::optional<Json> value =
	    parse(R"( {"n":null,"t":true,"f":false,"i":-12,"d":1.5e3,"s":"x","a":[1,"2"],"o":{}} )");
	REQUIRE(value && value->isObject());
	CHECK(value->find("n")->isNull());
	CHECK_EQ(value->find("t")->asBool(), true);
	CHECK_EQ(value->find("f")->asBool(), false);
	CHECK_EQ(value->find("i")->numberText(), std::string("-12"));
	CHECK_EQ(value->find("d")->numberText(), std::string("1.5e3"));
	CHECK_EQ(value->find("s")->asString(), std::string("x"));
	CHECK_EQ(value->find("a")->items().size(), std::size_t{2});
	CHECK(value->find("o")->isObject() && value->find("o")->members().empty());
	CHECK(value->find("missing") == nullptr);
}

TEST(JsonDepthLimitCountsTheOutermostAsOne) {
	CHECK(parse(nested('[', ']', kDepth)));
	CHECK(!parse(nested('[', ']', kDepth + 1)));
	CHECK(parse(nestedObjects(kDepth)));
	CHECK(!parse(nestedObjects(kDepth + 1)));
	CHECK(parse("[[1]]", 2));
	CHECK(!parse("[[1]]", 1));
	// 入れ子でない値は深さ 0。
	CHECK(parse("1", 0));
}

TEST(JsonDepthLimitStopsBeforeRecursingIntoHugeInput) {
	// 1 MiB の [ で、上限まで降りたところで失敗する（スタックを溢れさせない）。
	std::string error;
	CHECK(!Json::parse(std::string(1 << 20, '['), kDepth, &error));
	CHECK(error.find("入れ子が深すぎます") != std::string::npos);
	CHECK(error.find("65 バイト目") == 0);
}

TEST(JsonRejectsWhatEncodingJsonRejects) {
	for (const char *text : {"",
	                         " ",
	                         "{",
	                         "{\"a\"}",
	                         "{\"a\":}",
	                         "{\"a\":1,}",
	                         "[1,]",
	                         "[1 2]",
	                         "{'a':1}",
	                         "01",
	                         "-",
	                         "+1",
	                         ".5",
	                         "1.",
	                         "1e",
	                         "1e+",
	                         "NaN",
	                         "Infinity",
	                         "True",
	                         "nul",
	                         "\"\\x\"",
	                         "\"\\u12\"",
	                         "\"\\u12g4\"",
	                         "\"a\nb\"",
	                         "\"\t\"",
	                         "\"abc",
	                         "1 2",
	                         "{} x",
	                         "\xef\xbb\xbf{}",
	                         "{}\f",
	                         "[\x01]",
	                         "\xff",
	                         "{\"a\":1}/**/"}) {
		std::string error;
		if (Json::parse(text, kDepth, &error)) {
			VwCli::test::fail(__FILE__, __LINE__, std::string("should not parse: ") + text);
		} else {
			CHECK(!error.empty());
		}
	}
}

TEST(JsonDecodesEscapes) {
	std::optional<Json> value = parse(R"("\"\\\/\b\f\n\r\t\u0041\u00e9\u3042\ud83d\ude00")");
	REQUIRE(value);
	CHECK_EQ(value->asString(),
	         std::string("\"\\/\b\f\n\r\tA\xc3\xa9\xe3\x81\x82\xf0\x9f\x98\x80"));
}

TEST(JsonReplacesLoneSurrogates) {
	// 対にならないサロゲートは U+FFFD にし、続くエスケープは別に読む（encoding/json と同じ）。
	const std::string fffd = "\xef\xbf\xbd";
	CHECK_EQ(must(R"("\ud800")").asString(), fffd);
	CHECK_EQ(must(R"("\udc00")").asString(), fffd);
	CHECK_EQ(must(R"("\ud800\u0041")").asString(), fffd + "A");
	CHECK_EQ(must(R"("\ud800\ud800\udc00")").asString(), fffd + "\xf0\x90\x80\x80");
	CHECK_EQ(must(R"("\udc00\ud800")").asString(), fffd + fffd);
	CHECK_EQ(must(R"("\ud800x")").asString(), fffd + "x");
}

TEST(JsonReplacesInvalidUtf8BytewiseInStrings) {
	const std::string fffd = "\xef\xbf\xbd";
	CHECK_EQ(must("\"a\xff"
	              "b\"")
	             .asString(),
	         "a" + fffd + "b");
	// 途切れた 3 バイトの文字は、残ったバイトごとに U+FFFD（utf8.DecodeRune と同じ）。
	CHECK_EQ(must("\"\xe3\x81\"").asString(), fffd + fffd);
	// 長すぎる綴り・サロゲート・U+10FFFF を超えるもの。
	CHECK_EQ(must("\"\xc0\xaf\"").asString(), fffd + fffd);
	CHECK_EQ(must("\"\xed\xa0\x80\"").asString(), fffd + fffd + fffd);
	CHECK_EQ(must("\"\xf4\x90\x80\x80\"").asString(), fffd + fffd + fffd + fffd);
	// 正しい 2〜4 バイトの文字はそのまま。
	CHECK_EQ(must("\"\xc3\xa9\xe3\x81\x82\xf0\x9f\x98\x80\xf4\x8f\xbf\xbf\"").asString(),
	         std::string("\xc3\xa9\xe3\x81\x82\xf0\x9f\x98\x80\xf4\x8f\xbf\xbf"));
	// キーも同じ。
	CHECK(must("{\"a\xff\":1}").find("a" + fffd));
}

TEST(JsonKeepsTheLastOfDuplicateKeys) {
	std::optional<Json> value = parse(R"({"a":1,"b":2,"a":3})");
	REQUIRE(value);
	CHECK_EQ(value->members().size(), std::size_t{2});
	CHECK_EQ(value->members()[0].first, std::string("a"));
	CHECK_EQ(value->find("a")->numberText(), std::string("3"));
}

TEST(JsonKeepsTheLastOfDuplicateKeysAmongMany) {
	// キーが多いと索引で探す。並びは最初に現れた位置のまま、値は後のもの。
	std::string text = "{";
	for (int i = 0; i < 40; ++i) {
		text += "\"k" + std::to_string(i) + "\":" + std::to_string(i) + ",";
	}
	text += R"("k5":"five","k30":"thirty","k3":"three"})";
	Json value = must(text);
	CHECK_EQ(value.members().size(), std::size_t{40});
	CHECK_EQ(value.members()[5].first, std::string("k5"));
	CHECK_EQ(value.find("k5")->asString(), std::string("five"));
	CHECK_EQ(value.find("k30")->asString(), std::string("thirty"));
	CHECK_EQ(value.find("k3")->asString(), std::string("three"));
	CHECK_EQ(value.find("k39")->numberText(), std::string("39"));
}

TEST(JsonParsesAMebibyteOfKeysInLinearTime) {
	// 1 MiB の要求に 10 万のキーを並べても、同じキーを探すのが 2 乗の時間にならない
	// （並びを順に見ると、Vectorworks のメインスレッドを長く止める）。索引を使えば
	// 最適化なし・サニタイザ付きでも数秒かからず、並びを順に見ると最適化なしでも
	// 100 秒を超えるので、その間に上限を置く。
	std::string text = "{";
	for (int i = 0; i < 100000; ++i) {
		text += "\"" + std::to_string(i) + "\":0,";
	}
	text += R"("0":1})";
	auto start = std::chrono::steady_clock::now();
	Json value = must(text);
	auto elapsed = std::chrono::steady_clock::now() - start;
	CHECK(elapsed < std::chrono::seconds(20));
	CHECK_EQ(value.members().size(), std::size_t{100000});
	CHECK_EQ(value.find("0")->numberText(), std::string("1"));
}

TEST(JsonKeepsNumberSpellings) {
	for (const char *text : {"0", "-0", "12", "-1.5E+10", "1e-400",
	                         "123456789012345678901234567890", "0.000", "2E2"}) {
		Json value = must(text);
		CHECK(value.isNumber());
		CHECK_EQ(value.dump(), std::string(text));
	}
}

TEST(JsonReadsInt64OnlyFromIntegerSpellings) {
	CHECK_EQ(must("42").asInt64().value_or(0), std::int64_t{42});
	CHECK_EQ(must("-9223372036854775808").asInt64().value_or(0), INT64_MIN);
	CHECK_EQ(must("9223372036854775807").asInt64().value_or(0), INT64_MAX);
	CHECK(!must("9223372036854775808").asInt64());
	CHECK(!must("1.0").asInt64());
	CHECK(!must("1e2").asInt64());
	CHECK_EQ(Json::integer(-7).numberText(), std::string("-7"));
}

TEST(JsonDumpsCompactlyAndEscapes) {
	Json value = Json::object({{"ok", Json::boolean(true)},
	                           {"s", Json::string("a\"b\\c\n\x01\x7f/")},
	                           {"a", Json::array({Json(), Json::integer(1)})}});
	CHECK_EQ(value.dump(), std::string(R"({"ok":true,"s":"a\"b\\c\n\u0001)"
	                                   "\x7f"
	                                   R"(/","a":[null,1]})"));
}

TEST(JsonDumpReplacesInvalidUtf8) {
	// 道具が返す文字列（例外の文言など）が UTF-8 でなくても、応答は UTF-8 にする。
	CHECK_EQ(Json::string("a\x82\xa0z").dump(), std::string("\"a\xef\xbf\xbd\xef\xbf\xbdz\""));
}

TEST(JsonRoundTrips) {
	const char *text =
	    R"({"a":[1,-2.5e3,true,false,null,"x\u0000y",{"b":{}}],"c":"\u00e9","d":[[[]]]})";
	Json value = must(text);
	Json again = must(value.dump());
	CHECK_EQ(again, value);
	CHECK_EQ(again.dump(), value.dump());
}

TEST(JsonComparesObjectsIgnoringOrder) {
	CHECK_EQ(must(R"({"a":1,"b":[1,2]})"), must(R"({"b":[1,2],"a":1})"));
	CHECK(!(must(R"({"a":1})") == must(R"({"a":1,"b":2})")));
	CHECK(!(must("[1,2]") == must("[2,1]")));
	CHECK(!(must("1") == must("1.0")));
	CHECK(!(must("null") == must("false")));
}

TEST(JsonSetReplacesAnExistingKey) {
	Json value = Json::object({{"a", Json::integer(1)}, {"a", Json::integer(2)}});
	CHECK_EQ(value.dump(), std::string(R"({"a":2})"));
	value.set("b", Json::boolean(false));
	value.set("a", Json::string("x"));
	CHECK_EQ(value.dump(), std::string(R"({"a":"x","b":false})"));
	Json list = Json::array();
	list.push(Json());
	CHECK_EQ(list.dump(), std::string("[null]"));
}
