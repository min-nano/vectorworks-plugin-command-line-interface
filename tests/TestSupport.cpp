#include "TestSupport.h"

#include <fstream>
#include <iostream>
#include <iterator>
#include <random>
#include <vector>

namespace VwCli::test {

namespace {

struct Entry {
	const char *name;
	TestFn run;
};

std::vector<Entry> &registry() {
	static std::vector<Entry> entries;
	return entries;
}

int &failures() {
	static int count = 0;
	return count;
}

} // namespace

Registrar::Registrar(const char *name, TestFn run) noexcept { registry().push_back({name, run}); }

void fail(const char *file, int line, const std::string &what) {
	++failures();
	std::cerr << file << ":" << line << ": FAIL: " << what << "\n";
}

TempDir::TempDir() {
	std::random_device random;
	path_ = std::filesystem::temp_directory_path() /
	        ("vwcli-test-" + std::to_string(random()) + "-" + std::to_string(random()));
	std::filesystem::create_directories(path_);
}

TempDir::~TempDir() {
	std::error_code ignored;
	std::filesystem::remove_all(path_, ignored);
}

std::string TempDir::utf8Path() const { return utf8(path_); }

std::string TempDir::sub(const std::string &name) const { return utf8(path_ / name); }

std::string readFile(const std::filesystem::path &path) {
	std::ifstream in(path, std::ios::binary);
	if (!in) {
		fail(__FILE__, __LINE__, "cannot read " + utf8(path));
		return {};
	}
	return std::string(std::istreambuf_iterator<char>(in), std::istreambuf_iterator<char>());
}

std::string utf8(const std::filesystem::path &path) {
	std::u8string text = path.u8string();
	return std::string(text.begin(), text.end());
}

int runAll(int argc, char **argv) {
	std::string filter = argc > 1 ? argv[1] : "";
	int run = 0;
	for (const Entry &entry : registry()) {
		if (!filter.empty() && std::string(entry.name).find(filter) == std::string::npos) {
			continue;
		}
		int before = failures();
		entry.run();
		++run;
		std::cout << (failures() == before ? "ok   " : "FAIL ") << entry.name << "\n";
	}
	std::cout << run << " tests, " << failures() << " failures\n";
	return failures() == 0 && run > 0 ? 0 : 1;
}

} // namespace VwCli::test
