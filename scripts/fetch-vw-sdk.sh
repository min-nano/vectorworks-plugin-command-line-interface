#!/bin/sh
# プラグインのビルドに要る Vectorworks SDK を VW_SDK_DIR に用意する（docs/plugin/build-and-release.md
# 「CI」）。公開の URL から取るので Secrets は要らない。形は SDK リファレンスの scripts/fetch-vw-sdk.sh
# （plugin の用途）と同じで、ヘッダ・ライブラリ・BuildVWR だけを残す。
#
#   VW_SDK_DIR=… VW_SDK_URL=… scripts/fetch-vw-sdk.sh
#
# 要るものがすでに揃っていれば（キャッシュを復元した）何もしない。揃っていなければ取り直す。
set -eu

dir="${VW_SDK_DIR:?VW_SDK_DIR is not set}"
url="${VW_SDK_URL:?VW_SDK_URL is not set}"

case "${RUNNER_OS:-$(uname -s)}" in
Windows | MINGW* | MSYS* | CYGWIN*)
	parts="Include Source LibWin ToolsWin"
	need="LibWin/Release/VWSDK.lib LibWin/Release/VWMM.lib ToolsWin/BuildVWR/buildvwr.exe"
	;;
macOS | Darwin)
	parts="Include Source LibMac ToolsMac"
	need="LibMac/Release/libVWSDK.a ToolsMac/BuildVWR/BuildVWR"
	;;
*)
	echo "fetch-vw-sdk.sh: the SDK is only for macOS and Windows" >&2
	exit 1
	;;
esac

has_sdk() {
	for file in Include/VectorworksSDK.h $need; do
		[ -f "$dir/SDKLib/$file" ] || return 1
	done
}

if has_sdk; then
	echo "the SDK is already at $dir"
	exit 0
fi
rm -rf "$dir"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
curl -fL --retry 4 --retry-delay 5 -o "$work/sdk.zip" "$url"
# Windows のランナーの bash には unzip が無いことがあるので、7z も使えるようにする。
if command -v unzip >/dev/null 2>&1; then
	unzip -q "$work/sdk.zip" -d "$work/sdk"
else
	7z x -bso0 -bsp0 -o"$work/sdk" "$work/sdk.zip"
fi
# zip は全体を 1 階層のフォルダで包んでいることがある。mac の zip が混ぜる __MACOSX は避ける。
sdklib="$(find "$work/sdk" -type d -name SDKLib -not -path '*/__MACOSX/*' | head -n 1)"
if [ -z "$sdklib" ]; then
	echo "fetch-vw-sdk.sh: SDKLib is not in $url" >&2
	exit 1
fi
mkdir -p "$dir/SDKLib"
for part in $parts; do
	cp -R "$sdklib/$part" "$dir/SDKLib/$part"
done
if [ -f "$dir/SDKLib/ToolsMac/BuildVWR/BuildVWR" ]; then
	chmod +x "$dir/SDKLib/ToolsMac/BuildVWR/BuildVWR"
fi
if ! has_sdk; then
	echo "fetch-vw-sdk.sh: the SDK at $dir is missing files" >&2
	exit 1
fi
echo "the SDK is at $dir"
