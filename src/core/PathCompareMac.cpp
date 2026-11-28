#include "core/PathCompare.h"

#include <CoreFoundation/CoreFoundation.h>

namespace VwCli::core::path_compare {

namespace {

// normalized は UTF-8 を NFC にした CFString（呼び出し元が CFRelease する）。UTF-8 として読み取れ
// なければ nullptr（要求の文字列は解析により正しい UTF-8 となっているが、関数としては任意の入力を
// 受け付ける）。
CFMutableStringRef normalized(std::string_view text) {
	CFStringRef string =
	    ::CFStringCreateWithBytes(kCFAllocatorDefault, reinterpret_cast<const UInt8 *>(text.data()),
		                          static_cast<CFIndex>(text.size()), kCFStringEncodingUTF8, false);
	CFMutableStringRef copy =
	    string != nullptr ? ::CFStringCreateMutableCopy(kCFAllocatorDefault, 0, string) : nullptr;
	if (string != nullptr) {
		::CFRelease(string);
	}
	if (copy != nullptr) {
		::CFStringNormalize(copy, kCFStringNormalizationFormC);
	}
	return copy;
}

}

bool equivalent(std::string_view a, std::string_view b) {
	CFMutableStringRef left = normalized(a);
	CFMutableStringRef right = normalized(b);
	// 読み取れなければ、文字列が完全に一致するか否かで決定する。
	bool same = left != nullptr && right != nullptr
	                ? ::CFStringCompare(left, right, kCFCompareCaseInsensitive) == kCFCompareEqualTo
	                : a == b;
	if (left != nullptr) {
		::CFRelease(left);
	}
	if (right != nullptr) {
		::CFRelease(right);
	}
	return same;
}

}
