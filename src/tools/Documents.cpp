#include "PluginPrefix.h"

#include "core/DocumentTools.h"
#include "core/PathCompare.h"
#include "core/Quit.h"
#include "core/SaveTarget.h"
#include "tools/Text.h"
#include "tools/Tools.h"

#include <cstddef>
#include <cstdint>
#include <stdexcept>
#include <string>
#include <vector>

// 図面を操作するツール（documents・save・close）の SDK の側。SDK の呼び出しを
// core::Documents として渡すのみで、検証の順序・保存先の検証・前面を戻す手順・結果の形式は
// src/core/DocumentTools.cpp（docs/plugin/tools.md「図面を操作するツール」。Findings「Documents」の
// 「指定した図面を保存する・閉じる」）。

namespace VwCli::tools {

namespace {

// pathOf は図面のパス。SDK が返す文字列をそのまま返す（正規化しない。docs/plugin/tools.md
// 「documents」）。読み取れなければ空。
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
		// プラグインはパスを書き換えずに渡す（形式は core が事前に検証している）。
		VectorWorks::Filing::IFileIdentifierPtr file(VectorWorks::Filing::IID_FileIdentifier);
		if (!file || file->Set(TXString(path)) != kVCOMError_NoError) {
			throw std::runtime_error("保存先の IFileIdentifier を作れませんでした");
		}
		return static_cast<std::uint32_t>(gSDK->SaveActiveDocumentPath(file));
	}

	// CloseDocument は保存の確認を表示せずに閉じ、未保存の変更を破棄する。閉じても false を
	// 返すことがあるため、成否は core が閉じた後の一覧で判定する（Findings
	// 「CloseDocument() は false を返しながら閉じている」）。
	void closeActive() override { (void)gSDK->CloseDocument(); }

	// 一覧が空でも、CloseDocument で閉じた図面が内部に残存していれば真を返す（core の
	// clearClosedDocuments が使用する。Findings「Documents」の
	// 「閉じた図面が残り、終了で保存の確認が出る」）。パスは使用しない。
	bool hasActive() override {
		VectorWorks::Filing::IFileIdentifierPtr file(VectorWorks::Filing::IID_FileIdentifier);
		bool saved = false;
		return gSDK->GetActiveDocument(&file, saved);
	}
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
	// プラグインの OS の to の形式の規則と、実際の比較方法・ファイルシステムに問い合わせる関数を
	// 渡す（CI では検証できないため、実機の確認で、大文字と小文字のみが異なる to が
	// to_is_open になることを確認する）。
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

// quit は開いている図面の一覧（GetOpenFilesList）のみを読み取るため、図面を操作するツールと
// 同じインターフェースを使用する。
// 終了は依頼しない（終了の依頼を返すのみで、依頼するのは受付処理の外の Clock）。
core::Json quit(const core::Json &args, const core::ToolContext &context,
                core::ToolStatus &status) {
	SdkDocuments docs;
	return core::quit(args, context, docs, status);
}

}
