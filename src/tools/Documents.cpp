#include "PluginPrefix.h"

#include "Interfaces/VectorWorks/Filing/IExportPDF.h"

#include "core/DocumentTools.h"
#include "core/PathCompare.h"
#include "core/Quit.h"
#include "core/ReadTools.h"
#include "core/SaveTarget.h"
#include "tools/Text.h"
#include "tools/Tools.h"

#include <cstddef>
#include <cstdint>
#include <stdexcept>
#include <string>
#include <vector>

// 図面を操作するツール（documents・save・close・export_pdf）の SDK の側。SDK の呼び出しを
// core::Documents・core::SheetPdf として渡すのみで、検証の順序・保存先の検証・前面を戻す手順・
// 結果の形式は src/core/DocumentTools.cpp（docs/plugin/tools.md「図面を操作するツール」・
// 「export_pdf」。Findings「Documents」の「指定した図面を保存する・閉じる」・Findings
// 「PDF Export」）。

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

// SdkSheetPdf は IExportPDF で前面の図面のシートを書き出す（Findings「PDF Export」の「呼び方」）。
// ダイアログは出さず、undo の記録も開かない（書き出しは記録を残さない）。
class SdkSheetPdf final : public core::SheetPdf {
public:
	std::vector<std::string> sheets() override {
		std::vector<std::string> names;
		// レイヤの列挙は読み取りツールと同じ（GetDrawingHeader の子の内、型が kLayerNode のもの。
		// Findings「Layers and Stories」の「レイヤを列挙する」）。
		for (MCObjectHandle layer = gSDK->FirstMemberObj(gSDK->GetDrawingHeader());
		     layer != nullptr; layer = gSDK->NextObject(layer)) {
			if (gSDK->GetObjectTypeN(layer) != ::kLayerNode) {
				continue;
			}
			TVariableBlock value;
			Sint16 type = 0;
			if (!gSDK->GetObjectVariable(layer, ovLayerType, value) || !value.GetSint16(type) ||
			    type != core::kLayerSheet) {
				continue;
			}
			TXString name;
			gSDK->GetObjectName(layer, name);
			names.push_back(utf8(name));
		}
		return names;
	}

	std::uint32_t open(const std::string &path) override {
		if (!pdf_ || pdf_->GetOptions(saved_) != kVCOMError_NoError) {
			throw std::runtime_error("IExportPDF を作れませんでした");
		}
		// fOpenInViewer の既定は起動によって変わるため、毎回偽にする（書き出す度に PDF を開く
		// アプリが前面に出ないように）。範囲は、シートを用紙の大きさの 1 頁にする
		// eWholeAreaAsOnePage にする（eCurrentView は画面の大きさの頁になる）。他は利用者の設定の
		// まま（Findings「PDF Export」の「設定」）。
		VectorWorks::Filing::SExportPDFOptions options = saved_;
		options.fOpenInViewer = false;
		options.fExportRangeKind = VectorWorks::Filing::SExportPDFOptions::eWholeAreaAsOnePage;
		if (pdf_->SetOptions(options) != kVCOMError_NoError) {
			restore();
			throw std::runtime_error("PDF の書き出しの設定を変えられませんでした");
		}
		VectorWorks::Filing::IFileIdentifierPtr file(VectorWorks::Filing::IID_FileIdentifier);
		if (!file || file->Set(TXString(path)) != kVCOMError_NoError) {
			restore();
			throw std::runtime_error("書き出す先の IFileIdentifier を作れませんでした");
		}
		auto error = static_cast<std::uint32_t>(pdf_->OpenPDFDocument(file));
		if (error != 0) {
			restore();
		}
		return error;
	}

	std::uint32_t page(const std::string &sheet) override {
		return static_cast<std::uint32_t>(pdf_->ExportPDFPages(TXString(sheet)));
	}

	// ClosePDFDocument から戻った時点で書き終わっている（Findings「PDF Export」の結論）。
	std::uint32_t close() override {
		auto error = static_cast<std::uint32_t>(pdf_->ClosePDFDocument());
		restore();
		return error;
	}

private:
	// restore は退避した利用者の設定を戻す（fOpenInViewer は保存される設定であるため）。
	void restore() { (void)pdf_->SetOptions(saved_); }

	VectorWorks::Filing::IExportPDFPtr pdf_{VectorWorks::Filing::IID_ExportPDF};
	VectorWorks::Filing::SExportPDFOptions saved_;
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

core::Json exportPdf(const core::Json &args, const core::ToolContext & /*context*/,
                     core::ToolStatus &status) {
	SdkDocuments docs;
	SdkSheetPdf pdf;
	// 書き出す先の検証は save と同じ関数を渡す（形式の拡張子は core が .pdf にする）。
	core::SaveEnvironment environment{core::save_target::kNativeForm,
	                                  {&core::path_compare::equivalent, &core::save_target::probeTo,
	                                   &core::save_target::probeParent}};
	return core::exportPdf(args, docs, pdf, environment, status);
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
