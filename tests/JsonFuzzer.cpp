// core/Json の解析のファジングのエントリポイント（libFuzzer）。ASan・UBSan と共にビルドし、
// CI（cpp.yml）が定められた時間のみ実行する。シードは protocol/fixtures/requests（深い入れ子・
// 長い文字列・不正な UTF-8 を含む）。
//
// 検証事項: いかなる入力でも異常終了しない（スタックを溢れさせない）。読み取れたものは書き出して
// 読み直すと同じ値になり、再度書き出しても同じ綴りになる。

#include "core/Json.h"
#include "core/Protocol.h"

#include <cstddef>
#include <cstdint>
#include <cstdlib>
#include <string>
#include <string_view>

extern "C" int LLVMFuzzerTestOneInput(const std::uint8_t *data, std::size_t size) {
	using VwCli::core::Json;
	constexpr int kDepth = VwCli::core::protocol::kMaxNestingDepth;
	std::string_view text(reinterpret_cast<const char *>(data), size);
	std::optional<Json> value = Json::parse(text, kDepth);
	if (!value) {
		return 0;
	}
	std::string dumped = value->dump();
	std::optional<Json> again = Json::parse(dumped, kDepth);
	if (!again || !(*again == *value) || again->dump() != dumped) {
		std::abort();
	}
	return 0;
}
