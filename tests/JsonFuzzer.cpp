// core/Json の解析のファジングの入口（libFuzzer）。ASan・UBSan と共にビルドし、CI（test.yml）が
// 決まった時間だけ回す。種は protocol/fixtures/requests（深い入れ子・長い文字列・不正な UTF-8 を
// 含む）。
//
// 確かめること: どんな入力でも落ちない（スタックを溢れさせない）。読めたものは書き出して読み直すと
// 同じ値になり、もう一度書き出しても同じ綴りになる。

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
