#pragma once

// 無 SDK の単体テストの小さな枠と、テストが共に使うもの。外部のテストの枠組みに依らない。

#include "core/Json.h"

#include <filesystem>
#include <ostream>
#include <sstream>
#include <string>
#include <vector>

namespace VwCli::core {

// CHECK_EQ が失敗を示せるように、JSON として書き出す。
inline std::ostream &operator<<(std::ostream &out, const Json &value) {
	return out << value.dump();
}

} // namespace VwCli::core

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

} // namespace VwCli::test

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

#define CHECK_EQ(actual, expected)                                                                 \
	do {                                                                                           \
		const auto vwcliActual = (actual);                                                         \
		const auto vwcliExpected = (expected);                                                     \
		if (!(vwcliActual == vwcliExpected)) {                                                     \
			::VwCli::test::fail(__FILE__, __LINE__,                                                \
			                    std::string(#actual " == " #expected "\n    actual:   ") +         \
			                        ::VwCli::test::show(vwcliActual) +                             \
			                        "\n    expected: " + ::VwCli::test::show(vwcliExpected));      \
		}                                                                                          \
	} while (false)
