#pragma once

// 無 SDK の単体テストの小さな枠と、テストが共に使うもの。外部のテストの枠組みに依らない。

#include "core/Files.h"
#include "core/Json.h"
#include "core/Protocol.h"
#include "core/Serve.h"

#include <cstddef>
#include <cstdint>
#include <filesystem>
#include <memory>
#include <ostream>
#include <source_location>
#include <sstream>
#include <string>
#include <string_view>
#include <type_traits>
#include <vector>

namespace VwCli::core {

// CHECK_EQ が失敗を示せるように、JSON として書き出す。
inline std::ostream &operator<<(std::ostream &out, const Json &value) {
	return out << value.dump();
}

}

namespace VwCli::test {

using TestFn = void (*)();

// Registrar はテストを登録する。登録に失敗する（メモリが足りない）なら走らせようがないので、
// 例外を投げずに終わる（noexcept）。
struct Registrar {
	Registrar(const char *name, TestFn run) noexcept;
};

// fail は失敗を記録する（テストは止めずに続ける）。
void fail(const char *file, int line, const std::string &what);

// TempDir はテストごとの一時ディレクトリ。壊すと中身ごと消す。
class TempDir {
public:
	TempDir();
	~TempDir();
	TempDir(const TempDir &) = delete;
	TempDir &operator=(const TempDir &) = delete;

	const std::filesystem::path &path() const { return path_; }
	std::string utf8Path() const;
	// sub は中のパスを UTF-8 の文字列で返す。
	std::string sub(const std::string &name) const;

private:
	std::filesystem::path path_;
};

// readFile はファイルの中身をすべて読む。読めなければ失敗を記録して空を返す。
std::string readFile(const std::filesystem::path &path);

// utf8 はパスを UTF-8 の文字列にする。
std::string utf8(const std::filesystem::path &path);

// must は読めるはずのテキストを、要求と同じ深さの上限で読む。読めなければ呼んだ場所で失敗を記録して
// null を返す。値で返すので、式の中で使っても寿命を気にしなくてよい。
core::Json must(std::string_view text,
                std::source_location where = std::source_location::current());

// isTrue は value が真偽値の true か（無い・null・ほかの型は偽）。
inline bool isTrue(const core::Json &value) { return value.isBool() && value.asBool(); }

// kNow は受け付けに渡す、決まった今の時刻（ナノ秒）。
inline constexpr std::int64_t kNow = 1760000000000000000;

// kMaxResponseBytes は応答のファイルを読む上限。通信規則は応答の大きさを決めないので、
// 要求の上限より大きく取る。
inline constexpr std::size_t kMaxResponseBytes = core::protocol::kMaxRequestBytes * 2;

// reasonOf は失敗の詳細（detail）の reason。無ければ空。
std::string reasonOf(const core::Json &detail);
inline std::string reasonOf(const core::ToolStatus &status) { return reasonOf(status.detail); }

// Caller は呼ぶ側を真似る。待つ印を掴んでから要求を置き、壊すと放す。
class Caller {
public:
	explicit Caller(const TempDir &dir) : dir_(dir) {}

	void put(const std::string &id, const std::string &request);
	// response は応答を読む。無ければ null。
	core::Json response(const std::string &id) const;
	// member は応答のフィールドの値。無ければ null。
	core::Json member(const std::string &id, std::string_view key) const;
	core::Json result(const std::string &id) const { return member(id, "result"); }
	// ok は応答が成功か。応答が無ければ偽。
	bool ok(const std::string &id) const { return isTrue(member(id, "ok")); }
	std::string code(const std::string &id) const;
	// session は session_start の応答の占有の印。無ければ空。
	std::string session(const std::string &id) const;

private:
	const TempDir &dir_;
	std::vector<std::unique_ptr<core::files::Lock>> locks_;
};

// runAll は登録されたテストを走らせる。引数があれば、名前にその文字列を含むものだけ。
int runAll(int argc, char **argv);

template <typename T> std::string show(const T &value) {
	std::ostringstream out;
	out << value;
	return out.str();
}

template <typename T> std::string show(const std::vector<T> &values) {
	std::string out = "[";
	for (const T &value : values) {
		out += (out.size() > 1 ? ", " : "") + show(value);
	}
	return out + "]";
}

// isCString は T が C の文字列（文字の配列か、文字へのポインタ）か。
template <typename T>
inline constexpr bool isCString =
    std::is_same_v<std::decay_t<T>, const char *> || std::is_same_v<std::decay_t<T>, char *>;

// checkEq は CHECK_EQ の中身。actual == expected でなければ、両辺を示して失敗を記録する。
template <typename A, typename B>
void checkEq(const A &actual, const B &expected, const char *file, int line, const char *what) {
	// C の文字列どうしは中身ではなくアドレスを比べるので断る（片方を std::string にする）。
	static_assert(!(isCString<A> && isCString<B>), "CHECK_EQ: compare C strings as std::string");
	if (!(actual == expected)) {
		fail(file, line,
		     std::string(what) + "\n    actual:   " + show(actual) +
		         "\n    expected: " + show(expected));
	}
}

}

#define VWCLI_JOIN2(a, b) a##b
#define VWCLI_JOIN(a, b) VWCLI_JOIN2(a, b)

#define TEST(name)                                                                                 \
	static void name();                                                                            \
	static const ::VwCli::test::Registrar VWCLI_JOIN(name, Registrar)(#name, name);                \
	static void name()

#define CHECK(condition)                                                                           \
	do {                                                                                           \
		if (!(condition)) {                                                                        \
			::VwCli::test::fail(__FILE__, __LINE__, #condition);                                   \
		}                                                                                          \
	} while (false)

// REQUIRE は満たさなければ失敗を記録して、テストの関数から戻る（続けると無い値を読むとき）。
#define REQUIRE(condition)                                                                         \
	do {                                                                                           \
		if (!(condition)) {                                                                        \
			::VwCli::test::fail(__FILE__, __LINE__, #condition);                                   \
			return;                                                                                \
		}                                                                                          \
	} while (false)

// CHECK_EQ は両辺を checkEq の引数として渡す。引数は式の終わりまで生きるので、写さずに比べても
// 一時の値の中を指す参照（must(...).asString() など）も寿命の内で読める。両辺を求める順は
// 決まらない（関数の引数の評価順）ので、両辺に副作用のある式を書かない。
#define CHECK_EQ(actual, expected)                                                                 \
	::VwCli::test::checkEq((actual), (expected), __FILE__, __LINE__, #actual " == " #expected)
