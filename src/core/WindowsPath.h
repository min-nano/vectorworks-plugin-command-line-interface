#pragma once

// Windows のパスの変換（src/core の内部用。WIN32
// のときだけビルドする）。スプール（FilesWindows.cpp）と save
// の保存先の確かめ（SaveTargetWindows.cpp）が同じ処理を使い、\\?\ を付ける処理を書き写さない
// （docs/plugin/bridge.md「分け方」）。

#include <string>
#include <string_view>

namespace VwCli::core::windows_path {

// extended が付ける頭。ドライブからの絶対パスには \\?\ を、
// UNC（\\server\…）には \\?\UNC\ を付ける。
// makeDirectories は、この長さからルートの終わりを求める。
inline constexpr std::wstring_view kExtendedPrefix = L"\\\\?\\";
inline constexpr std::wstring_view kExtendedUncPrefix = L"\\\\?\\UNC\\";

// widen は UTF-8 を UTF-16 にする。
std::wstring widen(std::string_view text);

// narrow は UTF-16 を UTF-8 にする。
std::string narrow(std::wstring_view wide);

// extended はパスを \\?\ を付けた UTF-16 にする（スプールのファイルと保存先の確かめは、すべて
// これで開く・聞く）。
//
// 付けないと、CON・NUL・AUX・COM1 などの名前は（Windows 10 までは拡張子があっても）ファイルでは
// なくデバイスを指す。id の綴りはそうした名前を除かないので、\\?\ を付けて作られた NUL.req.json
// を読むつもりで、NUL デバイスを開くことになる。付けると名前を解釈せずにそのまま使う。
//
// \\?\ のパスは / を区切りとして読まず、. と .. も解かないので、/ を区切りに直す。スプールの場所は
// GetModuleFileNameW の返すモジュールの絶対パスから求め、. と .. を含まない（save の to は形の
// 確かめで . と .. と / を先に断る）。受け付けるのはドライブからの絶対パス（C:\…）・UNC
// （\\server\…）・\\?\ 付きのパスだけで、それ以外（ルート相対・ドライブ相対・\\.\ のデバイス
// 名前空間）は空を返して開かせない（予約された名前をデバイスとして開かないため）。
std::wstring extended(std::string_view path);

}
