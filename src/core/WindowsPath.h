#pragma once

// Windows のパスの変換（src/core の内部用。WIN32 の場合のみビルドする）。
// スプール（FilesWindows.cpp）と save の保存先の検証（SaveTargetWindows.cpp）が
// 同一の処理を使用し、\\?\ を付ける処理を複製しない（docs/plugin/bridge.md「分割の方針」）。

#include <string>
#include <string_view>

namespace VwCli::core::windows_path {

// extended が付ける接頭辞。ドライブからの絶対パスには \\?\ を、
// UNC（\\server\…）には \\?\UNC\ を付ける。
// makeDirectories は、この長さからルートの終端を算出する。
inline constexpr std::wstring_view kExtendedPrefix = L"\\\\?\\";
inline constexpr std::wstring_view kExtendedUncPrefix = L"\\\\?\\UNC\\";

// widen は UTF-8 を UTF-16 に変換する。
std::wstring widen(std::string_view text);

// narrow は UTF-16 を UTF-8 に変換する。
std::string narrow(std::wstring_view wide);

// extended はパスを \\?\ を付けた UTF-16 に変換する（スプールのファイルと保存先の検証は、全て
// これで開く・問い合わせる）。
//
// 付けなければ、CON・NUL・AUX・COM1 などの名前は（Windows 10 までは拡張子があっても）ファイルでは
// なくデバイスを指す。id の綴りはそうした名前を除外しないため、\\?\ を付けて作成された
// NUL.req.json を読み取る意図で、NUL デバイスを開くことになる。付ければ名前を解釈せずにそのまま
// 使用する。
//
// \\?\ のパスは / を区切りとして解釈せず、. と .. も解決しないため、/ を区切りに変換する。
// スプールの場所は GetModuleFileNameW の返すモジュールの絶対パスから導出し、. と .. を含まない
// （save の to は形式の検証で . と .. と / を先に拒否する）。受け付けるのはドライブからの絶対パス
// （C:\…）・UNC（\\server\…）・\\?\ 付きのパスのみで、それ以外（ルート相対・ドライブ相対・\\.\ の
// デバイス名前空間）は空を返して開かせない（予約された名前をデバイスとして開かないため）。
std::wstring extended(std::string_view path);

}
