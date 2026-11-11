#include "core/PathCompare.h"

#include <CoreFoundation/CoreFoundation.h>

namespace VwCli::core::path_compare {

namespace {

// normalized は UTF-8 を NFC にした CFString（呼ぶ側が CFRelease する）。UTF-8 として読めなければ
// nullptr（要求の文字列は解析で正しい UTF-8 になっているが、関数としては何でも受ける）。
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
	// 読めなければ、文字列が完全に一致するかで決める。
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
