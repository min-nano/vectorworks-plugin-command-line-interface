#include "PluginPrefix.h"

#include "core/DocumentTools.h"
#include "core/PathCompare.h"
#include "core/SaveTarget.h"
#include "tools/Text.h"
#include "tools/Tools.h"

#include <cstddef>
#include <cstdint>
#include <stdexcept>
#include <string>
#include <vector>

// 図面を扱う道具（documents・save・close）の SDK の側。SDK の呼び出しを core::Documents として
// 渡すだけで、確かめの順・保存先の確かめ・前面を戻す手順・結果の形は src/core/DocumentTools.cpp
// （docs/plugin/tools.md「図面を扱う道具」。Findings「Documents」の「指定した図面を保存する・閉じる」）。

namespace VwCli::tools {

namespace {

// pathOf は図面のパス。SDK が返す文字列をそのまま返す（正規化しない。docs/plugin/tools.md
// 「documents」）。読めなければ空。
std::string pathOf(const VectorWorks::Filing::IFileIdentifierPtr &file) {
	if (!file) {
		return {};
	}
	TXString path;
	if (file->GetFileFullPath(path) != kVCOMError_NoError) {
		return {};
	}
	return utf8(path);
}

class SdkDocuments final : public core::Documents {
public:
	std::vector<core::OpenDocument> list() override {
		VectorWorks::TVWArray_OpenFileInformation files;
		gSDK->GetOpenFilesList(files);
		std::vector<core::OpenDocument> out;
		for (std::size_t i = 0; i < files.GetSize(); ++i) {
			const VectorWorks::SOpenFileInformation &info = files.GetAt(i);
			out.push_back(core::OpenDocument{pathOf(info.fpFileID), info.fIsInMemoryOnly,
			                                 info.fIsActive,
			                                 static_cast<std::int32_t>(info.fFileRef)});
		}
		return out;
	}

	bool switchTo(std::int32_t ref) override { return gSDK->SwitchToOpenFile(ref); }

	std::uint32_t saveActive(const std::string &path) override {
		// プラグインはパスを書き換えずに渡す（形は core が先に確かめてある）。
		VectorWorks::Filing::IFileIdentifierPtr file(VectorWorks::Filing::IID_FileIdentifier);
		if (!file || file->Set(TXString(path)) != kVCOMError_NoError) {
			throw std::runtime_error("保存先の IFileIdentifier を作れませんでした");
		}
		return static_cast<std::uint32_t>(gSDK->SaveActiveDocumentPath(file));
	}

	// CloseDocument は保存の確認を出さずに閉じ、未保存の変更を捨てる。閉じても false を返すことが
	// あるので、成否は core が閉じたあとの一覧で決める（Findings「CloseDocument() は false を
	// 返しながら閉じている」）。
	void closeActive() override { (void)gSDK->CloseDocument(); }
};

}

core::Json documents(const core::Json &args, const core::ToolContext & /*context*/,
                     core::ToolStatus &status) {
	SdkDocuments docs;
	return core::documents(args, docs, status);
}

core::Json save(const core::Json &args, const core::ToolContext & /*context*/,
                core::ToolStatus &status) {
	SdkDocuments docs;
	// プラグインの OS の to の形の規則と、本物の比べ方・ファイルシステムに聞く関数を渡す（CI では
	// 確かめられないので、実機の確認で、大文字と小文字だけが違う to が to_is_open
	// になるのを見る）。
	core::SaveEnvironment environment{core::save_target::kNativeForm,
	                                  {&core::path_compare::equivalent, &core::save_target::probeTo,
	                                   &core::save_target::probeParent}};
	return core::save(args, docs, environment, status);
}

core::Json close(const core::Json &args, const core::ToolContext & /*context*/,
                 core::ToolStatus &status) {
	SdkDocuments docs;
	return core::close(args, docs, status);
}

}
