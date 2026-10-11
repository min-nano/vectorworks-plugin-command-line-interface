//
//	core/Serve.h
//
//	**受け付け 1 回の手順**と、占有の印（docs/protocol.md「占有」）。OS タイマー（src/Clock）が
//	刻みごとに Server::serve を 1 回呼ぶ。道具の表を引数で受けるので、偽の道具の表で無 SDK の
//	テストができる（tests/CoreServeTests.cpp・tests/ProtocolFixturesTests.cpp）。
//
//	手順（docs/plugin/bridge.md「受け付け 1 回」）:
//	  1. ロックを掴んでいなければ、用意して掴み、前の回の残骸を消す。取れなければ何もしない。
//	  2. 回の始めに並べた要求を名前の昇順で 1 件ずつ読んで消し、待つ印を確かめる。
//	  3. 読めない要求には invalid_request で応える。
//	  4. 占有に照らしてから道具を実行して応える。種類 app の道具は応えてから終了の依頼を
//	     返し、その回の残りを取り出さない。
//
//	受け付けの中にループを書かない（回の始めに並べた要求を処理したら戻る）。SDK 呼び出しは
//	メインスレッドからだけなので、受け付け用のスレッドも持たない。
//

#pragma once

#include "core/Bridge.h"
#include "core/Json.h"

#include <filesystem>
#include <functional>
#include <string>
#include <vector>

namespace VwCli::core
{
	// 道具の種類（docs/protocol.md「予約された道具」の kind）。
	enum class ToolKind
	{
		// 図面を読むだけ
		Read,
		// 図面を変える（変えるかが分からないものも）
		Write,
		// Vectorworks そのものに働く（quit）
		App,
	};

	// kind の綴り（read / write / app）。
	const char* toolKindName(ToolKind kind);

	// 道具が返す失敗。failed が真のときだけ応答を失敗にする。
	struct ToolError
	{
		bool failed = false;
		BridgeCode code = BridgeCode::Internal;
		// 日本語。応答の error にそのまま載る
		std::string message;

		// 引数の誤り（知らない引数・型や値の誤り・必須の欠け）。null を返すので
		// `return error.invalidArgs("…");` と書ける。
		Json invalidArgs(std::string why);
		// そのほか（図面が開かれていないなど、道具によって失敗になるかが違う理由）。
		Json internal(std::string why);
	};

	// 道具が受け付けから受け取る状態。
	struct ToolContext
	{
		// 占有の印を発行しているか（ping の session）
		bool session = false;
	};

	using ToolFn =
		std::function<Json(const Json& args, const ToolContext& context, ToolError& error)>;

	// 道具の表の 1 行（docs/plugin/bridge.md「道具の表」）。
	struct Tool
	{
		// [a-z0-9_]+(\.[a-z0-9_]+)*
		std::string name;
		// 日本語。呼ぶ側がそのまま読む
		std::string description;
		// JSON Schema（オブジェクトのテキスト）。tools の結果にそのまま載る
		std::string inputSchema;
		ToolKind kind = ToolKind::Read;
		ToolFn run;
	};

	using ToolTable = std::vector<Tool>;

	// 道具の名前の綴りか（[a-z0-9_]+(\.[a-z0-9_]+)*）。
	bool isValidToolName(const std::string& name);

	// 受け付けが自分で答える予約された道具か（tools・session_start・session_end）。ping も
	// 予約されているが、SDK に依る中身を持つので表に載せる。
	bool isBuiltinToolName(const std::string& name);

	// 表が正しいか（名前の綴り・重複・予約された名前・inputSchema がオブジェクトの JSON か・
	// run があるか）。テストで表を確かめるためのもの。
	bool validateToolTable(const ToolTable& tools, std::string& error);

	// 受け付けの状態。
	enum class ServePhase
	{
		// ロックを掴んで受け付けた
		Serving,
		// ロックを取れなかった（ほかの Vectorworks が受け付けている）
		Waiting,
		// スプールを用意できない・ロックを開けない（次の回にやり直す）
		Error,
	};

	// 受け付け 1 回の結果（docs/plugin/bridge.md「受け付けの結果」）。タイマーが見るのは
	// quit だけで、ほかはテストで手順を確かめるためにある。
	struct ServeResult
	{
		ServePhase phase = ServePhase::Waiting;
		// Error の理由、または応答を書けなかった理由
		std::string message;
		// 種類 app の道具を実行した（タイマーが受け付けの外で終了を頼む）
		bool quit = false;
	};

	// 受け付ける橋 1 つ。スプールのロックと占有の印（メモリだけ）を持つ。
	class Server
	{
	public:
		explicit Server(std::filesystem::path spoolDir);

		// 受け付け 1 回。
		ServeResult serve(const ToolTable& tools);

		// 占有の印を発行しているか。
		bool sessionActive() const
		{
			return !fSession.empty();
		}

		BridgeSpool& spool()
		{
			return fSpool;
		}

	private:
		BridgeSpool fSpool;
		// 発行している占有の印。空なら占有されていない
		std::string fSession;

		// 読めた要求 1 件に応える。種類 app の道具を実行したら ranApp に真を入れる。
		BridgeResponse answer(const BridgeRequest& request, const ToolTable& tools, bool& ranApp);
		// answer の例外を internal の応答にする。
		BridgeResponse answerSafely(const BridgeRequest& request, const ToolTable& tools,
									bool& ranApp);
		// 要求の印を占有に照らす。実行してよければ真。断るなら refusal に応答を入れる。
		bool admit(const std::string& session, bool needSession, BridgeResponse& refusal) const;
	};

	// 占有の印を作る（推測されにくい乱数。綴りは id と同じ）。
	std::string newSessionId();
} // namespace VwCli::core
