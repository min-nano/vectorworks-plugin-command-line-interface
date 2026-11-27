#include "core/Ping.h"

#include "core/Args.h"
#include "core/Protocol.h"

namespace VwCli::core {

Json ping(const Json &args, const ToolContext &context, const PingFacts &facts,
          ToolStatus &status) {
	args::Reader reader(args, kPingSpec.name, status);
	reader.onlyKnown({});
	if (!reader.ok()) {
		return Json();
	}
	Json layer = facts.currentLayer ? Json::string(*facts.currentLayer) : Json();
	return Json::object({{"version", Json::string(facts.version)},
	                     {"branch", Json::string(facts.branch)},
	                     {"protocol", Json::integer(protocol::kVersion)},
	                     {"pid", Json::integer(facts.pid)},
	                     {"session", Json::boolean(context.session)},
	                     {"document_open", Json::boolean(facts.currentLayer.has_value())},
	                     {"current_layer", std::move(layer)}});
}

}
