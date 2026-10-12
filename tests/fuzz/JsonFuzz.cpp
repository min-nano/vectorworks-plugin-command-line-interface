//
// JsonFuzz.cpp
//
// core/Json の解析の libFuzzer の入口（docs/plugin/plan.md 段 1「core/Json のファジング」）。
// 要求はプラグインの外が書いたテキストなので、どんな入力でも Vectorworks ごと落ちないことを
// ASan・UBSan の下で確かめる。あわせて、読めた値は書き出して読み直しても同じになること
// （書き出しが読めない JSON を作らないこと）を確かめる。
//
// 種は protocol/fixtures/ の要求の見本と tests/fuzz/seeds/（.github/workflows/test.yml が集める）。
//

#include "core/Bridge.h"
#include "core/Json.h"

#include <cstddef>
#include <cstdint>
#include <string>

extern "C" int LLVMFuzzerTestOneInput(const std::uint8_t* data, std::size_t size) {
	const std::string text(reinterpret_cast<const char*>(data), size);

	VwCli::core::Json value;
	std::string error;
	if (VwCli::core::Json::parse(text, value, error)) {
		const std::string dumped = value.dump();
		VwCli::core::Json again;
		if (!VwCli::core::Json::parse(dumped, again, error) || again.dump() != dumped ||
			!again.equals(value))
			__builtin_trap();
	} else if (error.empty()) {
		// 失敗には必ず理由を付ける。
		__builtin_trap();
	}

	VwCli::core::BridgeRequest request;
	if (VwCli::core::parseBridgeRequest(text, request, error) &&
		(request.tool.empty() || !request.args.isObject()))
		__builtin_trap();
	return 0;
}
