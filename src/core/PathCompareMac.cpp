#include "core/PathCompare.h"

#include <CoreFoundation/CoreFoundation.h>

namespace VwCli::core::path_compare {

namespace {

// normalized は UTF-8 を NFC にした CFString（呼ぶ側が CFRelease する）。作れなければ nullptr。
CFMutableStringRef normalized(std::string_view text) {
	CFStringRef string =
	    ::CFStringCreateWithBytes(kCFAllocatorDefault, reinterpret_cast<const UInt8 *>(text.data()),
		                          static_cast<CFIndex>(text.size()), kCFStringEncodingUTF8, false);
	if (string == nullptr) {
		return nullptr;
	}
	CFMutableStringRef copy = ::CFStringCreateMutableCopy(kCFAllocatorDefault, 0, string);
	::CFRelease(string);
	if (copy != nullptr) {
		::CFStringNormalize(copy, kCFStringNormalizationFormC);
	}
	return copy;
}

}

bool equivalent(std::string_view a, std::string_view b) {
	CFMutableStringRef left = normalized(a);
	CFMutableStringRef right = normalized(b);
	bool same = a == b;
	if (left != nullptr && right != nullptr) {
		same = ::CFStringCompare(left, right, kCFCompareCaseInsensitive) == kCFCompareEqualTo;
	}
	if (left != nullptr) {
		::CFRelease(left);
	}
	if (right != nullptr) {
		::CFRelease(right);
	}
	return same;
}

}
