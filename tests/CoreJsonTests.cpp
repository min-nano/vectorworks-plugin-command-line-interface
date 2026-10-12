//
// CoreJsonTests.cpp
//
// 最小 JSON（src/core/Json.h）の単体テスト。正常な往復だけでなく「壊れた入力で異常終了
// しない・受け付けない」ことを重点的に確かめる。**外から来たテキストを読む**処理であるため。
//

#include "TestFramework.h"
#include "core/Json.h"

#include <chrono>
#include <cstddef>
#include <limits>
#include <string>

using VwCli::core::isValidUtf8;
using VwCli::core::Json;
using VwCli::core::jsonQuote;
using VwCli::core::kJsonMaxDepth;

namespace {
// 読んで書き戻した 1 行を返す（往復の確認用）。
std::string RoundTrip(const std::string& text) {
    Json value;
    std::string error;
    if (!Json::parse(text, value, error))
        return "<失敗: " + error + ">";
    return value.dump();
}

bool Parses(const std::string& text) {
    Json value;
    std::string error;
    return Json::parse(text, value, error);
}

// 深さ depth の入れ子（外側を 1 と数える）。配列とオブジェクトを交互に重ねる。
std::string Nested(int depth) {
    std::string open;
    std::string close;
    for (int i = 0; i < depth; ++i) {
        if (i % 2 == 0) {
            open += "{\"a\":";
            close.insert(0, "}");
        } else {
            open += "[";
            close.insert(0, "]");
        }
    }
    return open + "1" + close;
}
} // namespace

TEST(json_dump_scalars) {
    CHECK_EQ(Json::null().dump(), "null");
    CHECK_EQ(Json::boolean(true).dump(), "true");
    CHECK_EQ(Json::boolean(false).dump(), "false");
    // 整数で表せる数は整数として書く。
    CHECK_EQ(Json::number(3.0).dump(), "3");
    CHECK_EQ(Json::integer(-12).dump(), "-12");
    CHECK_EQ(Json::string("あ").dump(), "\"あ\"");
}

TEST(json_dump_escapes) {
    // 引用符・傍線・制御文字だけをエスケープし、UTF-8 はそのまま通す。
    CHECK_EQ(jsonQuote("a\"b\\c"), "\"a\\\"b\\\\c\"");
    CHECK_EQ(jsonQuote("1\n2\t3"), "\"1\\n2\\t3\"");
    CHECK_EQ(jsonQuote(std::string("\x01")), "\"\\u0001\"");
    CHECK_EQ(jsonQuote("\r"), R"("\r")");
    CHECK_EQ(jsonQuote("\b"), R"("\b")");
    CHECK_EQ(jsonQuote("\f"), R"("\f")");
    CHECK_EQ(jsonQuote("1階"), "\"1階\"");
}

TEST(json_object_keeps_insertion_order) {
    Json value = Json::object();
    value.set("z", Json::integer(1));
    value.set("a", Json::integer(2));
    value.set("m", Json::integer(3));
    CHECK_EQ(value.dump(), "{\"z\":1,\"a\":2,\"m\":3}");

    // 同じ鍵を入れ直しても位置は動かない（値だけ替わる）。
    value.set("z", Json::integer(9));
    CHECK_EQ(value.dump(), "{\"z\":9,\"a\":2,\"m\":3}");
}

TEST(json_array_and_nesting) {
    Json items = Json::array();
    items.push(Json::string("1F"));
    items.push(Json::string("2F"));
    Json root = Json::object();
    root.set("layers", items);
    CHECK_EQ(root.dump(), "{\"layers\":[\"1F\",\"2F\"]}");
    CHECK(root.at("layers").isArray());
    CHECK_EQ(root.at("layers").items().size(), std::size_t(2));
    // 無い鍵は null。
    CHECK(root.at("missing").isNull());
    CHECK_EQ(root.at("missing").asString("既定"), std::string("既定"));
}

TEST(json_kind_mismatch_is_a_no_op) {
    Json number = Json::number(1);
    number.push(Json::integer(2));
    number.set("a", Json::integer(3));
    CHECK_EQ(number.dump(), "1");
    Json array = Json::array();
    array.set("a", Json::integer(1));
    CHECK_EQ(array.dump(), "[]");
    Json object = Json::object();
    object.push(Json::integer(1));
    CHECK_EQ(object.dump(), "{}");
    CHECK_EQ(number.asString("既定"), std::string("既定"));
    CHECK_EQ(number.asBool(true), true);
    CHECK_EQ(Json::string("x").asNumber(2.5), 2.5);
    CHECK(!Json::array().has("a"));
    CHECK(number.isNumber() && !number.isString() && !number.isBool());
}

TEST(json_parse_round_trip) {
    CHECK_EQ(RoundTrip("{\"a\":1,\"b\":[true,false,null],\"c\":\"あ\"}"),
        "{\"a\":1,\"b\":[true,false,null],\"c\":\"あ\"}");
    CHECK_EQ(RoundTrip("  [ 1 , 2 , 3 ]  "), "[1,2,3]");
    CHECK_EQ(RoundTrip(" \t\r\n{}"), "{}");
    CHECK_EQ(RoundTrip("[]"), "[]");
    CHECK_EQ(RoundTrip("\"\""), "\"\"");
}

TEST(json_parse_numbers) {
    Json value;
    std::string error;
    CHECK(Json::parse("[0, -0, 1, -2, 1.5, 2e3, -1.25e-2, 1E+2, 0.5]", value, error));
    CHECK_EQ(value.items().size(), std::size_t(9));
    CHECK_EQ(value.items()[2].asNumber(), 1.0);
    CHECK_EQ(value.items()[3].asNumber(), -2.0);
    CHECK_EQ(value.items()[4].asNumber(), 1.5);
    CHECK_EQ(value.items()[5].asNumber(), 2000.0);
    CHECK_EQ(value.items()[6].asNumber(), -0.0125);
    CHECK_EQ(value.items()[7].asNumber(), 100.0);
    CHECK_EQ(value.items()[8].asNumber(), 0.5);
}

TEST(json_parse_rejects_numbers_outside_the_grammar) {
    // RFC 8259 の number 以外は読まない（呼ぶ側の JSON の読み手と受け付ける要求を揃える）。
    for (const char* text : {"+1", "01", "-01", ".5", "1.", "1.e3", "-", "1e", "1e+", "0x10",
             "Infinity", "NaN", "--1", "1.5.2"}) {
        CHECK(!Parses(text));
    }
}

TEST(json_parse_rejects_numbers_out_of_range) {
    Json value;
    std::string error;
    CHECK(!Json::parse("1e999", value, error));
    CHECK(error.find("範囲") != std::string::npos);
    CHECK(!Json::parse("-1e999", value, error));
    CHECK(!Json::parse("123456789e301", value, error));
    CHECK(!Json::parse("1" + std::string(400, '0'), value, error));
    // 表せる最大の近くは読む。
    CHECK(Json::parse("1.7976931348623157e308", value, error));
    // 小さすぎる数は 0 にする（標準ライブラリによらず。Go の encoding/json と同じ）。
    for (const std::string& tiny : {std::string("1e-400"), std::string("-1e-400"),
             std::string("0.") + std::string(400, '0') + "1",
             std::string("0.0001e-99999999999999999999")}) {
        CHECK(Json::parse(tiny, value, error));
        CHECK_EQ(value.asNumber(), 0.0);
    }
    CHECK(Json::parse("4.9e-324", value, error));
    CHECK(value.asNumber() > 0.0);
    // 指数の桁が長くても、仮数の位と合わせて読む。
    CHECK(Json::parse("0." + std::string(400, '0') + "1e000000000000000000403", value, error));
    CHECK_EQ(value.asNumber(), 100.0);
    CHECK(!Json::parse("1e+99999999999999999999", value, error));
}

TEST(json_parse_string_escapes) {
    Json value;
    std::string error;
    CHECK(Json::parse("\"a\\\"b\\\\c\\n\"", value, error));
    CHECK_EQ(value.asString(), std::string("a\"b\\c\n"));
    CHECK(Json::parse(R"("\/\b\f\n\r\t")", value, error));
    CHECK_EQ(value.asString(), std::string("/\b\f\n\r\t"));

    // \u は UTF-8 へ（1〜4 バイト。大文字の 16 進も読む）。サロゲート対は 1 文字にまとめる。
    CHECK(Json::parse(R"("\u0041\u00E9\u3042")", value, error));
    CHECK_EQ(value.asString(), std::string("Aéあ"));
    CHECK(Json::parse("\"\\u67f1\"", value, error));
    CHECK_EQ(value.asString(), std::string("柱"));
    CHECK(Json::parse("\"\\ud83d\\ude00\"", value, error));
    CHECK_EQ(value.asString(), std::string("\U0001F600"));

    // 知らない綴り・傍線で終わる・\u の桁が足りない／16 進でないものは受け付けない。
    CHECK(!Parses(R"("\q")"));
    CHECK(!Parses("\"\\"));
    CHECK(!Parses(R"("\u12")"));
    CHECK(!Parses(R"("\uZZZZ")"));
}

TEST(json_parse_lone_surrogates_become_replacement_characters) {
    // 組にならないサロゲートは U+FFFD にする（読んだ文字列を常に正しい UTF-8 に保つ）。
    const std::string replacement = "\xEF\xBF\xBD";
    Json value;
    std::string error;
    CHECK(Json::parse(R"("\ud83dx")", value, error));
    CHECK_EQ(value.asString(), replacement + "x");
    CHECK(Json::parse(R"("\ude00")", value, error));
    CHECK_EQ(value.asString(), replacement);
    // 上位の後ろの \u が下位でない → 上位を置き換え、続く \u は 1 文字として読む。
    CHECK(Json::parse(R"("\ud83d\u0041")", value, error));
    CHECK_EQ(value.asString(), replacement + "A");
    CHECK(Json::parse(R"("\ud83d\ud83d\ude00")", value, error));
    CHECK_EQ(value.asString(), replacement + "\U0001F600");
    CHECK(isValidUtf8(value.asString()));
    // 上位の後ろの \u が壊れている → 受け付けない。
    CHECK(!Parses(R"("\ud83d\uZZZZ")"));
}

TEST(json_parse_rejects_raw_control_characters_in_strings) {
    CHECK(!Parses(std::string("\"a\nb\"")));
    CHECK(!Parses(std::string("\"a\tb\"")));
    CHECK(!Parses(std::string("\"a\x01") + "b\""));
    // 0x7F は制御文字だが JSON では許される。
    CHECK(Parses(std::string("\"a\x7F") + "b\""));
}

TEST(json_parse_rejects_broken_input) {
    Json value;
    std::string error;
    for (const char* text : {"", " ", "{", "[1,2", "{\"a\"}", "{\"a\":}", "{a:1}", "truex", "tru",
             "nul", "\"abc", "[1,2] xx", "01a", "{\"a\":1,}", "[1,]", "[,1]", "{\"a\":1 \"b\":2}",
             "[1 2]", "{\"a\":@}", "[@]", "'a'", "\xEF\xBB\xBF{}"}) {
        error.clear();
        CHECK(!Json::parse(text, value, error));
        CHECK(!error.empty());
    }
}

TEST(json_parse_failure_leaves_output_untouched) {
    Json value = Json::string("前");
    std::string error;
    CHECK(!Json::parse("[1,", value, error));
    CHECK_EQ(value.asString(), std::string("前"));
}

TEST(json_parse_nesting_limit) {
    // 外側を 1 と数えて 64 までは読み、65 は読まない（docs/protocol.md「要求」）。
    CHECK_EQ(kJsonMaxDepth, 64);
    CHECK(Parses(Nested(kJsonMaxDepth)));
    CHECK(!Parses(Nested(kJsonMaxDepth + 1)));
    CHECK(Parses(std::string(64, '[') + std::string(64, ']')));
    CHECK(!Parses(std::string(65, '[') + std::string(65, ']')));

    // 深い入れ子で再帰させない（閉じていなくても、上限で止まる）。
    Json value;
    std::string error;
    CHECK(!Json::parse(std::string(1U << 20U, '['), value, error));
    CHECK(error.find("深すぎます") != std::string::npos);

    // 値（スカラー）は数えない。
    CHECK(Parses("1"));
    CHECK(Parses("[[1]]"));
}

TEST(json_parse_requires_valid_utf8) {
    CHECK(Parses("\"あ\""));
    CHECK(Parses("\"\U0001F600\""));
    // 途中で切れた・続きのバイトが無い・冗長な表現・サロゲート・範囲外は読まない。
    for (const std::string& text : {std::string("\"\xE3\x81\""), std::string("\"\x80\""),
             std::string("\"\xC0\xAF\""), std::string("\"\xE0\x80\xAF\""),
             std::string("\"\xED\xA0\x80\""), std::string("\"\xF4\x90\x80\x80\""),
             std::string("\"\xFF\""), std::string("\"\xF0\x9F\x98\"")}) {
        Json value;
        std::string error;
        CHECK(!Json::parse(text, value, error));
        CHECK(error.find("UTF-8") != std::string::npos);
    }
}

TEST(json_is_valid_utf8) {
    CHECK(isValidUtf8(""));
    CHECK(isValidUtf8("abc"));
    CHECK(isValidUtf8("\xC2\x80"));
    CHECK(isValidUtf8("\xEF\xBF\xBD"));
    CHECK(isValidUtf8("\xF4\x8F\xBF\xBF"));
    CHECK(!isValidUtf8("\xC1\xBF"));
    CHECK(!isValidUtf8("\xE0\x9F\xBF"));
    CHECK(!isValidUtf8("\xED\xBF\xBF"));
    CHECK(!isValidUtf8("\xF0\x8F\xBF\xBF"));
    CHECK(!isValidUtf8("\xF5\x80\x80\x80"));
    CHECK(!isValidUtf8("\xC2"));
    CHECK(!isValidUtf8("\xE3\x81\x41"));
}

TEST(json_parse_duplicate_keys_last_wins) {
    // 同じ鍵は後のものが勝ち、位置は最初のまま（Json::set と同じ）。
    Json value;
    std::string error;
    CHECK(Json::parse(R"({"a":1,"b":2,"a":3})", value, error));
    CHECK_EQ(value.dump(), "{\"a\":3,\"b\":2}");
}

TEST(json_parse_many_keys_is_not_quadratic) {
    // 1 MiB の中に鍵を十数万並べても、受け付けを長く止めない。
    std::string text = "{";
    for (int i = 0; i < 100000; ++i) {
        if (i > 0)
            text += ',';
        text += "\"k" + std::to_string(i) + "\":0";
    }
    text += '}';
    const auto start = std::chrono::steady_clock::now();
    Json value;
    std::string error;
    CHECK(Json::parse(text, value, error));
    CHECK_EQ(value.members().size(), std::size_t(100000));
    const auto spent = std::chrono::steady_clock::now() - start;
    // 線形なら（サニタイザ付きでも）1 秒に遠く届かない。2 乗なら数十秒かかる。
    CHECK(spent < std::chrono::seconds(10));
}

TEST(json_dump_non_integral_and_non_finite) {
    CHECK_EQ(Json::number(1.5).dump(), "1.5");
    CHECK_EQ(Json::number(-0.0125).dump(), "-0.0125");
    CHECK(Json::number(1e300).dump().find('e') != std::string::npos);
    const double awkward = 0.1 + 0.2;
    Json parsed;
    std::string error;
    CHECK(Json::parse(Json::number(awkward).dump(), parsed, error));
    CHECK_EQ(parsed.asNumber(), awkward);
    // 最大値の近くは 15 桁に丸めると範囲を超えるので、読み戻せる桁数で書く（ファジングで
    // 見つかった。読み戻すと同じ値になる）。
    for (const double edge :
        {std::numeric_limits<double>::max(), -std::numeric_limits<double>::max(),
            std::numeric_limits<double>::denorm_min(), std::numeric_limits<double>::min()}) {
        CHECK(Json::parse(Json::number(edge).dump(), parsed, error));
        CHECK_EQ(parsed.asNumber(), edge);
    }
    const double inf = std::numeric_limits<double>::infinity();
    CHECK_EQ(Json::number(inf).dump(), "null");
    CHECK_EQ(Json::number(-inf).dump(), "null");
    CHECK_EQ(Json::number(std::numeric_limits<double>::quiet_NaN()).dump(), "null");
}

TEST(json_equals_ignores_member_order) {
    Json a;
    Json b;
    std::string error;
    CHECK(Json::parse(R"({"x":[1,{"y":true}],"z":null})", a, error));
    CHECK(Json::parse(R"({"z":null,"x":[1,{"y":true}]})", b, error));
    CHECK(a.equals(b));
    CHECK(Json::parse(R"({"z":null,"x":[1,{"y":false}]})", b, error));
    CHECK(!a.equals(b));
    CHECK(Json::parse(R"({"x":[1,{"y":true}]})", b, error));
    CHECK(!a.equals(b));
    CHECK(Json::parse(R"({"x":[1,{"y":true}],"w":null})", b, error));
    CHECK(!a.equals(b));
    CHECK(!Json::integer(1).equals(Json::string("1")));
    CHECK(!Json::array().equals(Json::object()));
    CHECK(Json::null().equals(Json::null()));
    CHECK(!Json::boolean(true).equals(Json::boolean(false)));
    CHECK(Json::string("あ").equals(Json::string("あ")));
    CHECK(!Json::parse("[1,2]", a, error) || !a.equals(Json::array()));
}

TEST_MAIN();
