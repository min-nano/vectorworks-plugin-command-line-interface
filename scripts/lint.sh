#!/usr/bin/env bash
#
# lint.sh - run locally the checks that .github/workflows/lint.yml enforces:
# treefmt (every formatter in treefmt.toml), clang-tidy (src/core/), cmake-lint,
# actionlint, yamllint, editorconfig-checker and the shell script checker.
#
#   scripts/lint.sh          # check; non-zero exit if anything is off (treefmt still
#                            # writes the formatted files: see them with git diff)
#   scripts/lint.sh --fix    # format in place (treefmt, clang-tidy), then check the rest
#
# A tool that is not installed is reported and skipped, so a partial local run
# still works; CI remains the complete gate. treefmt runs the formatters by their
# plain names (clang-format, cmake-format, gofmt, shfmt, ruff) found on PATH; the
# versions CI uses are pinned in lint.yml. Pin clang-tidy with CLANG_SUFFIX, e.g.
#   CLANG_SUFFIX=-18 scripts/lint.sh

set -uo pipefail

cd "$(dirname "$0")/.." || exit 1

SUFFIX="${CLANG_SUFFIX:-}"
CLANG_TIDY="clang-tidy${SUFFIX}"
FIX=0
[ "${1:-}" = "--fix" ] && FIX=1

FAILED=0
SKIPPED=()

have() { command -v "$1" >/dev/null 2>&1; }

# skip <tool> <install-hint>: note a missing tool and keep going.
skip() {
	echo "==> $1: not installed, skipping ($2)"
	SKIPPED+=("$1")
}

# report <label>: turn the previous command's exit status into OK or a failure.
report() {
	if [ "$?" -eq 0 ]; then
		echo "    OK"
	else
		echo "    FAILED: $1"
		FAILED=1
	fi
}

CMAKE_FILES=(CMakeLists.txt tests/CMakeLists.txt tests/fuzz/CMakeLists.txt)

# treefmt fails on the first formatter it cannot find, so it needs all of them.
if have treefmt; then
	echo "==> treefmt (treefmt.toml)"
	if [ "$FIX" -eq 1 ]; then
		treefmt
		report "treefmt"
	else
		treefmt --ci
		report "treefmt"
	fi
else
	skip "treefmt" "https://github.com/numtide/treefmt/releases (and the formatters in treefmt.toml)"
fi

# The file list must stay identical to the one in lint.yml: every SDK-free
# translation unit.
if have "$CLANG_TIDY" && have cmake; then
	TIDY_FILES=(src/core/*.cpp)
	echo "==> clang-tidy (${#TIDY_FILES[@]} translation units)"
	if cmake -S . -B build-lint -DCMAKE_BUILD_TYPE=Debug -DVW_BUILD_TESTS=ON \
		-DCMAKE_EXPORT_COMPILE_COMMANDS=ON >/dev/null; then
		TIDY_ARGS=(-p build-lint --warnings-as-errors='*')
		[ "$FIX" -eq 1 ] && TIDY_ARGS+=(--fix --fix-errors)
		"$CLANG_TIDY" "${TIDY_ARGS[@]}" "${TIDY_FILES[@]}"
		report "clang-tidy"
	else
		false
		report "clang-tidy (cmake configure)"
	fi
else
	skip "$CLANG_TIDY" "apt-get install clang-tidy cmake"
fi

if have cmake-lint; then
	echo "==> cmake-lint"
	cmake-lint "${CMAKE_FILES[@]}"
	report "cmake-lint"
else
	skip "cmake-lint" "pip install cmakelang pyyaml"
fi

if have actionlint; then
	echo "==> actionlint"
	actionlint
	report "actionlint"
else
	skip "actionlint" "go install github.com/rhysd/actionlint/cmd/actionlint@latest"
fi

if have shellcheck; then
	echo "==> shellcheck (scripts/*.sh)"
	shellcheck scripts/*.sh
	report "shellcheck"
else
	skip "shellcheck" "apt-get install shellcheck"
fi

if have yamllint; then
	echo "==> yamllint"
	yamllint --strict .
	report "yamllint"
else
	skip "yamllint" "pip install yamllint"
fi

EC=""
if have editorconfig-checker; then
	EC=editorconfig-checker
elif have ec; then
	EC=ec
fi
if [ -n "$EC" ]; then
	echo "==> editorconfig-checker"
	"$EC"
	report "editorconfig-checker"
else
	EC_MODULE="github.com/editorconfig-checker/editorconfig-checker/v3"
	skip "editorconfig-checker" "go install ${EC_MODULE}/cmd/editorconfig-checker@latest"
fi

echo
if [ "${#SKIPPED[@]}" -gt 0 ]; then
	echo "Skipped (not installed): ${SKIPPED[*]}"
fi
if [ "$FAILED" -ne 0 ]; then
	echo "Some lint checks FAILED."
	exit 1
fi
echo "All available lint checks passed."
