//
//	SpoolHelpers.h
//
//	スプールのテストの道具立て。一時ディレクトリと、呼ぶ側の真似（待つ印を掴む・
//	bridge.lock を掴む）を持つ。掴み方は docs/protocol.md「ロック」「待つ印」の表のとおりで、
//	同じプロセスの中でも別の記述子（POSIX の flock は開いたファイルの記述ごと）・別の
//	ハンドル（Windows の共有なし）として働くので、プラグインの手順をそのまま確かめられる。
//

#pragma once

#include "core/Bridge.h"

#include <algorithm>
#include <atomic>
#include <chrono>
#include <cstdio>
#include <filesystem>
#include <fstream>
#include <iterator>
#include <sstream>
#include <string>
#include <system_error>
#include <vector>

#if defined(_WIN32)
#	ifndef NOMINMAX
#		define NOMINMAX
#	endif
#	ifndef WIN32_LEAN_AND_MEAN
#		define WIN32_LEAN_AND_MEAN
#	endif
#	include <windows.h>
#else
#	include <fcntl.h>
#	include <sys/file.h>
#	include <unistd.h>
#endif

namespace vwtest
{
	// テストごとの一時ディレクトリ。壊すときに中身ごと消す。
	class TempDir
	{
	public:
		TempDir()
		{
			static std::atomic<int> counter{0};
			const auto stamp = std::chrono::steady_clock::now().time_since_epoch().count();
			std::ostringstream name;
			name << "vwcli-test-" << stamp << "-" << counter++;
			fPath = std::filesystem::temp_directory_path() / name.str();
			std::filesystem::create_directories(fPath);
		}
		~TempDir()
		{
			std::error_code ec;
			std::filesystem::remove_all(fPath, ec);
		}
		TempDir(const TempDir&) = delete;
		TempDir& operator=(const TempDir&) = delete;
		TempDir(TempDir&&) = delete;
		TempDir& operator=(TempDir&&) = delete;

		const std::filesystem::path& path() const
		{
			return fPath;
		}

	private:
		std::filesystem::path fPath;
	};

	// 呼ぶ側の真似: ファイルを掴み続ける（待つ印・bridge.lock）。壊すか release で放す。
	class HeldFile
	{
	public:
		explicit HeldFile(const std::filesystem::path& path)
		{
#if defined(_WIN32)
			fHandle = CreateFileW(path.c_str(), GENERIC_READ | GENERIC_WRITE, 0, nullptr,
								  OPEN_ALWAYS, FILE_ATTRIBUTE_NORMAL, nullptr);
#else
			fFd = ::open(path.c_str(), O_RDWR | O_CREAT | O_CLOEXEC, 0600);
			if (fFd >= 0 && ::flock(fFd, LOCK_EX | LOCK_NB) != 0)
			{
				::close(fFd);
				fFd = -1;
			}
#endif
		}
		~HeldFile()
		{
			release();
		}
		HeldFile(const HeldFile&) = delete;
		HeldFile& operator=(const HeldFile&) = delete;
		HeldFile(HeldFile&&) = delete;
		HeldFile& operator=(HeldFile&&) = delete;

		bool held() const
		{
#if defined(_WIN32)
			return fHandle != INVALID_HANDLE_VALUE;
#else
			return fFd >= 0;
#endif
		}

		void release()
		{
#if defined(_WIN32)
			if (fHandle != INVALID_HANDLE_VALUE)
			{
				CloseHandle(fHandle);
				fHandle = INVALID_HANDLE_VALUE;
			}
#else
			if (fFd >= 0)
			{
				::close(fFd);
				fFd = -1;
			}
#endif
		}

	private:
#if defined(_WIN32)
		HANDLE fHandle = INVALID_HANDLE_VALUE;
#else
		int fFd = -1;
#endif
	};

	inline void WriteFile(const std::filesystem::path& path, const std::string& text)
	{
		std::ofstream out(path, std::ios::binary | std::ios::trunc);
		out.write(text.data(), static_cast<std::streamsize>(text.size()));
	}

	inline std::string ReadFile(const std::filesystem::path& path)
	{
		std::ifstream in(path, std::ios::binary);
		return {std::istreambuf_iterator<char>(in), std::istreambuf_iterator<char>()};
	}

	inline bool Exists(const std::filesystem::path& path)
	{
		std::error_code ec;
		return std::filesystem::exists(path, ec);
	}

	// ディレクトリの中の名前（bridge.lock を除く）を昇順で。
	inline std::vector<std::string> ListNames(const std::filesystem::path& dir)
	{
		std::vector<std::string> names;
		std::error_code ec;
		for (std::filesystem::directory_iterator it(dir, ec), end; !ec && it != end;
			 it.increment(ec))
		{
			const std::string name = VwCli::core::utf8FromPath(it->path().filename());
			if (name != VwCli::core::kBridgeLockFile)
				names.push_back(name);
		}
		std::sort(names.begin(), names.end());
		return names;
	}

	// 名前の並びを 1 行に（CHECK_EQ の表示のため）。
	inline std::string Joined(const std::vector<std::string>& names)
	{
		std::string out;
		for (const std::string& name : names)
		{
			if (!out.empty())
				out += ' ';
			out += name;
		}
		return out;
	}
} // namespace vwtest
