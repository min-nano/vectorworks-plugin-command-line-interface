#!/usr/bin/env python3
#
# make-seeds.py OUT_DIR - write the seeds for the core/Json fuzzer (JsonFuzz.cpp).
#
# The seeds are the request examples of protocol/fixtures/serve/ plus inputs that
# aim at the guards of core/Json: deep nesting around the limit of 64, a long
# string near the 1 MiB request limit, invalid UTF-8, escapes and numbers at the
# edges of double. They are generated rather than checked in, because some are not
# valid UTF-8 (which .editorconfig forbids in the tree) or are large.

import base64
import json
import pathlib
import sys

ROOT = pathlib.Path(__file__).resolve().parents[2]


def fixture_requests():
	"""Yield the request texts of protocol/fixtures/serve/*.json."""
	for path in sorted((ROOT / "protocol" / "fixtures" / "serve").glob("*.json")):
		fixture = json.loads(path.read_text(encoding="utf-8"))
		for number, round_ in enumerate(fixture["rounds"]):
			for request in round_.get("requests", []):
				name = f"{path.stem}-{number}-{request['id']}"
				if "json" in request:
					text = json.dumps(request["json"], ensure_ascii=False).encode()
				elif "text" in request:
					text = request["text"].encode()
				elif "base64" in request:
					text = base64.b64decode(request["base64"])
				else:
					# padded / nested are generated below at a smaller size.
					continue
				yield name, text


def edge_cases():
	"""Yield inputs aimed at the guards of core/Json."""
	yield "deep-64", b"[" * 64 + b"]" * 64
	yield "deep-65", b"[" * 65 + b"]" * 65
	yield "deep-open", b"[" * 4096
	yield "deep-object", b'{"a":' * 63 + b"1" + b"}" * 63
	yield "long-string", b'{"tool":"ping","args":{"pad":"' + b"x" * (1 << 20) + b'"}}'
	yield "many-keys", b"{" + b",".join(b'"k%d":0' % i for i in range(5000)) + b"}"
	yield "duplicate-keys", b'{"a":1,"b":2,"a":3}'
	yield "invalid-utf8", b'{"tool":"echo","args":{"s":"\xff\xc0\xaf\xed\xa0\x80\xf4\x90\x80\x80"}}'
	yield "truncated-utf8", b'"\xe3\x81'
	u = lambda code: b"\\u" + code.encode()
	yield "escapes", (
		b'"' + u("d83d") + u("de00") + b" " + u("d83d") + u("0041") + b" "
		+ u("de00") + b" " + u("0000") + rb' \/\b\f\n\r\t\"\\"'
	)
	yield "numbers", (
		b"[0,-0,1e-400,-1e-400,1e308,1.7976931348623157e308,4.9e-324,"
		b"123456789012345678901234567890,0.1]"
	)
	yield "number-overflow", b"[1e309]"
	yield "values", '{"a":[1,-2.5e-3,true,false,null,"あ"],"b":{}}'.encode()


def main():
	out = pathlib.Path(sys.argv[1])
	out.mkdir(parents=True, exist_ok=True)
	count = 0
	for name, text in [*fixture_requests(), *edge_cases()]:
		(out / f"{name}.json").write_bytes(text)
		count += 1
	print(f"wrote {count} seeds to {out}")


if __name__ == "__main__":
	main()
