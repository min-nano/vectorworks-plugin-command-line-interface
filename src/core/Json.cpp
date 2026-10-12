//
// core/Json.cpp
//
// 最小 JSON の実装（意図と制約は core/Json.h）。書き出しは再帰、読み取りは深さの上限つきの
// 再帰下降。**外から来たテキストを読む**ので、壊れた入力で異常終了しないことを最優先にする。
//

#include "core/Json.h"

#include <algorithm>
#include <cmath>
#include <cstddef>
#include <limits>
#include <locale>
#include <sstream>
#include <string>
#include <string_view>
#include <unordered_map>
#include <utility>
#include <vector>

#if defined(_WIN32)
#    include <cstdlib>
#    include <locale.h>
#elif defined(__APPLE__)
#    include <cstdlib>
#    include <xlocale.h>
#else
#    include <clocale>
#    include <cstdlib>
#endif

namespace VwCli::core {
namespace {
// 置き換え文字（U+FFFD）。組にならないサロゲートの \u をこれにする（読んだ文字列を
// 常に正しい UTF-8 に保つ。Go の encoding/json と同じ扱い）。
constexpr unsigned int kReplacementCharacter = 0xFFFDU;

// 数のテキストを double へ（"C" ロケールで。小数点は常に '.'）。範囲を超えて大きい
// ものは false。小さすぎるものは非正規化数か 0 になる。
//
// std::istringstream は使わない。非正規化数（4.9e-324 など）を libc++（mac。プラグインが
// 動く場所）は失敗にし、libstdc++ は読むので、同じ要求の読み方が OS で変わる。strtod_l は
// どの OS でも正しく丸めて読む。
bool ParseDouble(const std::string& text, double& out) {
#if defined(_WIN32)
    static const _locale_t classic = _create_locale(LC_NUMERIC, "C");
    out = _strtod_l(text.c_str(), nullptr, classic);
#else
    static const locale_t classic = newlocale(LC_NUMERIC_MASK, "C", locale_t{});
    out = strtod_l(text.c_str(), nullptr, classic);
#endif
    return std::isfinite(out);
}

// 指数の桁が長すぎる数の指数を、値を変えない範囲で詰める。C ランタイムによっては指数を
// int で数えてあふれさせ、1e-99999999999999999999 を無限大と読む（Wine の msvcrt で
// 確かめた）。仮数の桁で位がずれるのは要求の上限（1 MiB）の桁数までなので、それを
// 十分に超える 16777216 で打ち切っても読む値は変わらない。
std::string ClampExponent(std::string_view text) {
    constexpr long long kLimit = 16777216;
    const std::size_t mark = text.find_first_of("eE");
    if (mark == std::string_view::npos)
        return std::string(text);
    std::size_t i = mark + 1;
    std::string sign;
    if (text[i] == '-' || text[i] == '+')
        sign = std::string(1, text[i++]);
    long long exponent = 0;
    for (; i < text.size(); ++i)
        exponent = std::min(exponent * 10 + (text[i] - '0'), kLimit);
    return std::string(text.substr(0, mark)) + "e" + sign + std::to_string(exponent);
}

// at() が「無ければ null」を返すための実体。
const Json& NullValue() {
    static const Json value;
    return value;
}

// --- 書き出し ------------------------------------------------------

void AppendEscaped(std::string& out, const std::string& text) {
    out += '"';
    for (const char raw : text) {
        const auto ch = static_cast<unsigned char>(raw);
        switch (ch) {
        case '"':
            out += "\\\"";
            break;
        case '\\':
            out += "\\\\";
            break;
        case '\n':
            out += "\\n";
            break;
        case '\r':
            out += "\\r";
            break;
        case '\t':
            out += "\\t";
            break;
        case '\b':
            out += "\\b";
            break;
        case '\f':
            out += "\\f";
            break;
        default:
            if (ch < 0x20) {
                // 制御文字は \u00XX。ここだけ 16 進で書く。
                static const char* const kHex = "0123456789abcdef";
                out += "\\u00";
                out += kHex[(ch >> 4U) & 0x0FU];
                out += kHex[ch & 0x0FU];
            } else {
                // UTF-8 はそのまま通す（日本語のレイヤ名を \u へ展開する理由が無い）。
                out += raw;
            }
            break;
        }
    }
    out += '"';
}

void AppendNumber(std::string& out, double value) {
    // 非有限は JSON で表せない。null に置き換える（1 つの異常値で応答全体を捨てない）。
    if (!std::isfinite(value)) {
        out += "null";
        return;
    }
    // 整数で表せるものは整数として書く（件数が 3.000000 と出ると読みにくい）。
    if (value == std::floor(value) && std::abs(value) < 1e15) {
        out += std::to_string(static_cast<long long>(value));
        return;
    }
    // 短く書けるなら短く書く。15 → 16 → 17 桁の順に試し、読み戻して同じ値になった
    // 最初のものを採る（17 桁まで行けば必ず一致するので、値は失われない）。読み戻しの
    // 失敗は一致と数えない（最大値の近くを 15 桁に丸めると範囲を超える。ファジングで
    // 見つかった）。
    for (int digits = 15; digits <= std::numeric_limits<double>::max_digits10; ++digits) {
        std::ostringstream oss;
        oss.imbue(std::locale::classic());
        oss.precision(digits);
        oss << value;
        const std::string text = oss.str();

        double back = 0.0;
        if ((ParseDouble(text, back) && back == value) ||
            digits == std::numeric_limits<double>::max_digits10) {
            out += text;
            return;
        }
    }
}

void AppendValue(std::string& out, const Json& value) {
    switch (value.kind()) {
    case Json::Kind::Null:
        out += "null";
        return;
    case Json::Kind::Bool:
        out += value.asBool() ? "true" : "false";
        return;
    case Json::Kind::Number:
        AppendNumber(out, value.asNumber());
        return;
    case Json::Kind::String:
        AppendEscaped(out, value.asString());
        return;
    case Json::Kind::Array: {
        out += '[';
        bool first = true;
        for (const Json& item : value.items()) {
            if (!first)
                out += ',';
            first = false;
            AppendValue(out, item);
        }
        out += ']';
        return;
    }
    case Json::Kind::Object: {
        out += '{';
        bool first = true;
        for (const auto& member : value.members()) {
            if (!first)
                out += ',';
            first = false;
            AppendEscaped(out, member.first);
            out += ':';
            AppendValue(out, member.second);
        }
        out += '}';
        return;
    }
    }
}

bool IsDigit(char ch) {
    return ch >= '0' && ch <= '9';
}
} // namespace

// -----------------------------------------------------------------------
// 再帰下降（RFC 8259）。fPos が次に読む位置で、失敗したら fError に理由を入れて false を
// 返す。depth は値を囲む入れ子の数で、オブジェクトか配列に入るたびに 1 増える。
class JsonParser {
public:
    explicit JsonParser(std::string_view text) : fText(text) {}

    bool run(Json& out) {
        skipSpace();
        if (!value(out, 0))
            return false;
        skipSpace();
        if (fPos != fText.size())
            return fail("末尾に余分な文字があります");
        return true;
    }

    const std::string& error() const {
        return fError;
    }

private:
    std::string_view fText;
    std::size_t fPos = 0;
    std::string fError;

    bool fail(const std::string& why) {
        if (fError.empty())
            fError = why + "（" + std::to_string(fPos) + " バイト目）";
        return false;
    }

    bool atEnd() const {
        return fPos >= fText.size();
    }

    char peek() const {
        return fPos < fText.size() ? fText[fPos] : '\0';
    }

    void skipSpace() {
        while (fPos < fText.size()) {
            const char ch = fText[fPos];
            if (ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r')
                ++fPos;
            else
                break;
        }
    }

    bool literal(std::string_view word, Json made, Json& out) {
        if (fText.substr(fPos, word.size()) != word)
            return fail("知らない綴りです");
        fPos += word.size();
        out = std::move(made);
        return true;
    }

    bool value(Json& out, int depth) {
        if (atEnd())
            return fail("値がありません");
        switch (peek()) {
        case '{':
        case '[':
            // **再帰を降りる前に深さを確かめる**（深い入れ子でスタックを溢れさせない）。
            if (depth + 1 > kJsonMaxDepth)
                return fail("入れ子が深すぎます（上限 " + std::to_string(kJsonMaxDepth) + "）");
            return peek() == '{' ? objectValue(out, depth + 1) : arrayValue(out, depth + 1);
        case '"': {
            std::string text;
            if (!stringValue(text))
                return false;
            out = Json::string(std::move(text));
            return true;
        }
        case 't':
            return literal("true", Json::boolean(true), out);
        case 'f':
            return literal("false", Json::boolean(false), out);
        case 'n':
            return literal("null", Json::null(), out);
        default:
            return numberValue(out);
        }
    }

    bool objectValue(Json& out, int depth) {
        // '{'
        ++fPos;
        Json made = Json::object();
        // 鍵 → fMembers の位置。同じ鍵は後のものが勝つ（Json::set と同じ扱い）。set の
        // 線形探索で足すと、1 MiB の中に鍵を十数万並べた要求で受け付けが長く止まるため。
        std::unordered_map<std::string, std::size_t> index;
        skipSpace();
        if (peek() == '}') {
            ++fPos;
            out = std::move(made);
            return true;
        }
        for (;;) {
            skipSpace();
            if (peek() != '"')
                return fail("鍵は文字列でなければなりません");
            std::string key;
            if (!stringValue(key))
                return false;
            skipSpace();
            if (peek() != ':')
                return fail("鍵の後に ':' がありません");
            ++fPos;
            skipSpace();
            Json member;
            if (!value(member, depth))
                return false;
            const auto found = index.find(key);
            if (found != index.end()) {
                made.fMembers[found->second].second = std::move(member);
            } else {
                index.emplace(key, made.fMembers.size());
                made.fMembers.emplace_back(std::move(key), std::move(member));
            }
            skipSpace();
            if (peek() == ',') {
                ++fPos;
                continue;
            }
            if (peek() == '}') {
                ++fPos;
                out = std::move(made);
                return true;
            }
            return fail("',' か '}' が要ります");
        }
    }

    bool arrayValue(Json& out, int depth) {
        // '['
        ++fPos;
        Json made = Json::array();
        skipSpace();
        if (peek() == ']') {
            ++fPos;
            out = std::move(made);
            return true;
        }
        for (;;) {
            skipSpace();
            Json item;
            if (!value(item, depth))
                return false;
            made.fItems.push_back(std::move(item));
            skipSpace();
            if (peek() == ',') {
                ++fPos;
                continue;
            }
            if (peek() == ']') {
                ++fPos;
                out = std::move(made);
                return true;
            }
            return fail("',' か ']' が要ります");
        }
    }

    // \u の後の 16 進 4 桁を読む。
    bool hex4(unsigned int& code) {
        if (fPos + 4 > fText.size())
            return fail("\\u の後が足りません");
        code = 0;
        for (std::size_t i = 0; i < 4; ++i) {
            const char ch = fText[fPos + i];
            unsigned int digit = 0;
            if (ch >= '0' && ch <= '9')
                digit = static_cast<unsigned int>(ch - '0');
            else if (ch >= 'a' && ch <= 'f')
                digit = static_cast<unsigned int>(ch - 'a') + 10U;
            else if (ch >= 'A' && ch <= 'F')
                digit = static_cast<unsigned int>(ch - 'A') + 10U;
            else
                return fail("\\u の後は 16 進 4 桁です");
            code = (code << 4U) | digit;
        }
        fPos += 4;
        return true;
    }

    // 符号位置を UTF-8 で足す。
    static void appendUtf8(std::string& out, unsigned int code) {
        if (code < 0x80U) {
            out += static_cast<char>(code);
        } else if (code < 0x800U) {
            out += static_cast<char>(0xC0U | (code >> 6U));
            out += static_cast<char>(0x80U | (code & 0x3FU));
        } else if (code < 0x10000U) {
            out += static_cast<char>(0xE0U | (code >> 12U));
            out += static_cast<char>(0x80U | ((code >> 6U) & 0x3FU));
            out += static_cast<char>(0x80U | (code & 0x3FU));
        } else {
            out += static_cast<char>(0xF0U | (code >> 18U));
            out += static_cast<char>(0x80U | ((code >> 12U) & 0x3FU));
            out += static_cast<char>(0x80U | ((code >> 6U) & 0x3FU));
            out += static_cast<char>(0x80U | (code & 0x3FU));
        }
    }

    // \u の 1 文字分（'u' の後から）。上位サロゲートの後に下位の \u が続けば 1 文字に
    // まとめ、組にならないサロゲートは置き換え文字にする。
    bool unicodeEscape(std::string& out) {
        unsigned int code = 0;
        if (!hex4(code))
            return false;
        const bool high = code >= 0xD800U && code <= 0xDBFFU;
        const bool low = code >= 0xDC00U && code <= 0xDFFFU;
        if (low) {
            appendUtf8(out, kReplacementCharacter);
            return true;
        }
        if (!high) {
            appendUtf8(out, code);
            return true;
        }
        if (fText.substr(fPos, 2) != "\\u") {
            appendUtf8(out, kReplacementCharacter);
            return true;
        }
        const std::size_t mark = fPos;
        fPos += 2;
        unsigned int next = 0;
        if (!hex4(next))
            return false;
        if (next >= 0xDC00U && next <= 0xDFFFU) {
            appendUtf8(out, 0x10000U + ((code - 0xD800U) << 10U) + (next - 0xDC00U));
            return true;
        }
        // 組にならなかった。上位は置き換え文字にし、続く \u は改めて 1 文字として読む。
        appendUtf8(out, kReplacementCharacter);
        fPos = mark;
        return true;
    }

    bool stringValue(std::string& out) {
        // '"'
        ++fPos;
        out.clear();
        while (!atEnd()) {
            const char ch = fText[fPos];
            if (ch == '"') {
                ++fPos;
                return true;
            }
            if (static_cast<unsigned char>(ch) < 0x20U)
                return fail("文字列に制御文字がそのまま入っています");
            if (ch != '\\') {
                out += ch;
                ++fPos;
                continue;
            }
            // '\'
            ++fPos;
            if (atEnd())
                return fail("傍線の後が足りません");
            const char esc = fText[fPos++];
            switch (esc) {
            case '"':
                out += '"';
                break;
            case '\\':
                out += '\\';
                break;
            case '/':
                out += '/';
                break;
            case 'b':
                out += '\b';
                break;
            case 'f':
                out += '\f';
                break;
            case 'n':
                out += '\n';
                break;
            case 'r':
                out += '\r';
                break;
            case 't':
                out += '\t';
                break;
            case 'u':
                if (!unicodeEscape(out))
                    return false;
                break;
            default:
                return fail("知らない傍線の綴りです");
            }
        }
        return fail("文字列が閉じていません");
    }

    // 数（RFC 8259 の number）。'+'・先頭の余分な 0・'.' の前後の欠けは読まない。
    bool numberValue(Json& out) {
        const std::size_t start = fPos;
        if (peek() == '-')
            ++fPos;
        if (peek() == '0') {
            ++fPos;
        } else if (IsDigit(peek())) {
            while (IsDigit(peek()))
                ++fPos;
        } else {
            return fail("値として読めません");
        }
        if (peek() == '.') {
            ++fPos;
            if (!IsDigit(peek()))
                return fail("小数点の後に桁がありません");
            while (IsDigit(peek()))
                ++fPos;
        }
        if (peek() == 'e' || peek() == 'E') {
            ++fPos;
            if (peek() == '-' || peek() == '+')
                ++fPos;
            if (!IsDigit(peek()))
                return fail("指数の桁がありません");
            while (IsDigit(peek()))
                ++fPos;
        }
        // 文法は確かめてあるので、読めないのは double の範囲を超えて大きいときだけ。小さすぎる
        // 数（1e-400 など）は 0 になる（Go の encoding/json と同じ）。
        double number = 0.0;
        if (!ParseDouble(ClampExponent(fText.substr(start, fPos - start)), number))
            return fail("数が表せる範囲を超えています");
        out = Json::number(number);
        return true;
    }
};

// -----------------------------------------------------------------------
Json Json::null() {
    return {};
}

Json Json::boolean(bool value) {
    Json made;
    made.fKind = Kind::Bool;
    made.fBool = value;
    return made;
}

Json Json::number(double value) {
    Json made;
    made.fKind = Kind::Number;
    made.fNumber = value;
    return made;
}

Json Json::integer(long long value) {
    return number(static_cast<double>(value));
}

Json Json::string(std::string value) {
    Json made;
    made.fKind = Kind::String;
    made.fString = std::move(value);
    return made;
}

Json Json::array() {
    Json made;
    made.fKind = Kind::Array;
    return made;
}

Json Json::object() {
    Json made;
    made.fKind = Kind::Object;
    return made;
}

bool Json::asBool(bool fallback) const {
    return fKind == Kind::Bool ? fBool : fallback;
}

double Json::asNumber(double fallback) const {
    return fKind == Kind::Number ? fNumber : fallback;
}

std::string Json::asString(const std::string& fallback) const {
    return fKind == Kind::String ? fString : fallback;
}

const Json& Json::at(const std::string& key) const {
    for (const auto& member : fMembers)
        if (member.first == key)
            return member.second;
    return NullValue();
}

bool Json::has(const std::string& key) const {
    return std::any_of(fMembers.begin(), fMembers.end(),
        [&key](const std::pair<std::string, Json>& member) { return member.first == key; });
}

void Json::push(Json value) {
    if (fKind != Kind::Array)
        return;
    fItems.push_back(std::move(value));
}

void Json::set(std::string key, Json value) {
    if (fKind != Kind::Object)
        return;
    for (auto& member : fMembers) {
        if (member.first == key) {
            member.second = std::move(value);
            return;
        }
    }
    fMembers.emplace_back(std::move(key), std::move(value));
}

std::string Json::dump() const {
    std::string out;
    AppendValue(out, *this);
    return out;
}

bool Json::parse(const std::string& text, Json& out, std::string& error) {
    error.clear();
    if (!isValidUtf8(text)) {
        error = "UTF-8 として読めません";
        return false;
    }
    JsonParser parser(text);
    Json made;
    if (!parser.run(made)) {
        error = parser.error();
        return false;
    }
    out = std::move(made);
    return true;
}

bool Json::equals(const Json& other) const {
    if (fKind != other.fKind)
        return false;
    switch (fKind) {
    case Kind::Null:
        return true;
    case Kind::Bool:
        return fBool == other.fBool;
    case Kind::Number:
        return fNumber == other.fNumber;
    case Kind::String:
        return fString == other.fString;
    case Kind::Array:
        return std::equal(fItems.begin(), fItems.end(), other.fItems.begin(), other.fItems.end(),
            [](const Json& a, const Json& b) { return a.equals(b); });
    case Kind::Object:
        if (fMembers.size() != other.fMembers.size())
            return false;
        return std::all_of(
            fMembers.begin(), fMembers.end(), [&other](const std::pair<std::string, Json>& member) {
                return other.has(member.first) && member.second.equals(other.at(member.first));
            });
    }
    return false;
}

std::string jsonQuote(const std::string& text) {
    std::string out;
    AppendEscaped(out, text);
    return out;
}

bool isValidUtf8(const std::string& text) {
    std::size_t i = 0;
    const std::size_t size = text.size();
    while (i < size) {
        const auto lead = static_cast<unsigned char>(text[i]);
        if (lead < 0x80U) {
            ++i;
            continue;
        }
        // 続くバイトの数と、2 バイト目に許す範囲（冗長な表現・サロゲート・U+10FFFF を
        // 超えるものを除く。RFC 3629 の表）。
        std::size_t extra = 0;
        unsigned char low = 0x80U;
        unsigned char high = 0xBFU;
        if (lead >= 0xC2U && lead <= 0xDFU) {
            extra = 1;
        } else if (lead >= 0xE0U && lead <= 0xEFU) {
            extra = 2;
            if (lead == 0xE0U)
                low = 0xA0U;
            else if (lead == 0xEDU)
                high = 0x9FU;
        } else if (lead >= 0xF0U && lead <= 0xF4U) {
            extra = 3;
            if (lead == 0xF0U)
                low = 0x90U;
            else if (lead == 0xF4U)
                high = 0x8FU;
        } else {
            return false;
        }
        if (size - i <= extra)
            return false;
        for (std::size_t k = 1; k <= extra; ++k) {
            const auto next = static_cast<unsigned char>(text[i + k]);
            const unsigned char min = k == 1 ? low : 0x80U;
            const unsigned char max = k == 1 ? high : 0xBFU;
            if (next < min || next > max)
                return false;
        }
        i += extra + 1;
    }
    return true;
}
} // namespace VwCli::core
