#include "TestSupport.h"

#include "core/Json.h"

#include <charconv>
#include <cstdint>
#include <filesystem>
#include <limits>
#include <string>
#include <string_view>
#include <system_error>

// 整数の引数を値で読む口（Json::integerValue）と、道具の結果の実数を書き出す口（Json::real）。
// 決まりは docs/plugin/tools.md「最初に提供する道具」の冒頭。

using VwCli::core::IntegerCheck;
using VwCli::core::IntegerValue;
using VwCli::core::Json;
using VwCli::test::must;

namespace {

IntegerValue read(std::string_view number) { return must(number).integerValue(); }

bool integer(std::string_view number, std::int64_t value) {
	IntegerValue got = read(number);
	return got.check == IntegerCheck::Integer && got.value == value;
}

bool fraction(std::string_view number) { return read(number).check == IntegerCheck::Fraction; }

bool outOfRange(std::string_view number) { return read(number).check == IntegerCheck::OutOfRange; }

}

TEST(IntegerValueAcceptsIntegersWrittenAnyWay) {
	CHECK(integer("2", 2));
	CHECK(integer("2.0", 2));
	CHECK(integer("2e0", 2));
	CHECK(integer("2E+0", 2));
	CHECK(integer("20e-1", 2));
	CHECK(integer("0.2e1", 2));
	CHECK(integer("0", 0));
	CHECK(integer("1000", 1000));
	CHECK(integer("1e3", 1000));
	CHECK(integer("2147483647", 2147483647));
	CHECK(integer("2.147483647e9", 2147483647));
}

TEST(IntegerValueReadsZeroWrittenAnyWay) {
	CHECK(integer("-0", 0));
	CHECK(integer("-0.0", 0));
	CHECK(integer("0e99999999", 0));
	CHECK(integer("0e-99999999", 0));
	CHECK(integer("0.000e1000000000000000000000", 0));
}

// 範囲と小数部は、仮数の最初の 0 でない数字の位置に指数を足した実際の桁の位置で決める。
TEST(IntegerValueUsesThePlaceOfTheDigits) {
	CHECK(integer("0.0000000001e10", 1));
	CHECK(integer("1" + std::string(10000, '0') + "e-10000", 1));
	CHECK(integer("0." + std::string(10000, '0') + "1e10001", 1));
	CHECK(integer("2e0000000000000000000000", 2));
	CHECK(integer("1000000000e-9", 1));
}

// 浮動小数点数に直すと 2 に丸まる値も、小数部があるとして断る。
TEST(IntegerValueRejectsFractions) {
	CHECK(fraction("2.5"));
	CHECK(fraction("2.0000000000000001"));
	CHECK(fraction("1e-1"));
	CHECK(fraction("-0.5"));
	CHECK(fraction("1e-999999999999999999"));
	CHECK(fraction("12345e-3"));
}

// 浮動小数点数に直すと無限大になる値も、範囲の外として断る。
TEST(IntegerValueRejectsValuesOutOfRange) {
	CHECK(outOfRange("2147483648"));
	CHECK(outOfRange("1e30"));
	CHECK(outOfRange("1e400"));
	CHECK(outOfRange("-1"));
	CHECK(outOfRange("-1e0"));
	CHECK(outOfRange("9999999999"));
	CHECK(outOfRange("10000000000"));
	CHECK(outOfRange("1e999999999999999999"));
	CHECK(outOfRange("1" + std::string(100000, '0')));
}

// 実数と綴りの組の見本（tests/fixtures/reals.json）。Go のテストは同じ組を json.Marshal と照らす
// （cli/internal/toolfixtures）。
TEST(RealMatchesTheFixture) {
	std::filesystem::path path = std::filesystem::path(VWCLI_TOOL_FIXTURES_DIR) / "reals.json";
	Json fixture = must(VwCli::test::readFile(path));
	const Json *reals = fixture.find("reals");
	REQUIRE(reals && reals->isArray() && !reals->items().empty());
	for (const Json &pair : reals->items()) {
		REQUIRE(pair.isObject() && pair.members().size() == 2);
		const Json *value = pair.find("value");
		const Json *text = pair.find("text");
		REQUIRE(value && value->isString() && text && text->isString());
		const std::string &spelling = value->asString();
		double number = 0;
		auto [end, ec] =
		    std::from_chars(spelling.data(), spelling.data() + spelling.size(), number);
		REQUIRE(ec == std::errc() && end == spelling.data() + spelling.size());
		CHECK_EQ(Json::real(number).dump(), text->asString());
	}
}

TEST(RealIsNullUnlessFinite) {
	CHECK(Json::real(std::numeric_limits<double>::infinity()).isNull());
	CHECK(Json::real(-std::numeric_limits<double>::infinity()).isNull());
	CHECK(Json::real(std::numeric_limits<double>::quiet_NaN()).isNull());
	CHECK(Json::real(1.5).isNumber());
}

// 書き出した綴りは JSON の数として読み直せ、同じ値に戻る。
TEST(RealRoundTrips) {
	for (double value : {0.1, 2366.025, 1e21, 1e-7, 5e-324, 1.7976931348623157e308, -43.065}) {
		std::string text = Json::real(value).dump();
		Json parsed = must(text);
		REQUIRE(parsed.isNumber());
		double back = 0;
		const std::string &spelling = parsed.numberText();
		std::from_chars(spelling.data(), spelling.data() + spelling.size(), back);
		CHECK_EQ(back, value);
	}
}
