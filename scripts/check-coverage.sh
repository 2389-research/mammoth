#!/usr/bin/env bash
# ABOUTME: Coverage threshold checker for mammoth.
# ABOUTME: Runs tests, parses coverage, fails if any critical package is below its threshold.

set -eo pipefail

# --- Configuration ---
PREFIX="github.com/2389-research/mammoth"

# Package thresholds (bash 3.2 compatible - no associative arrays).
# Based on measured coverage as of 2026-02-07, set 2-3 points below actual.
PACKAGES="llm/sse attractor agent llm cmd/mammoth"
threshold_for() {
    case "$1" in
        llm/sse)       echo 95 ;;
        attractor)     echo 85 ;;
        agent)         echo 80 ;;
        llm)           echo 80 ;;
        cmd/mammoth)   echo 65 ;;
        *)             echo 0  ;;
    esac
}
OVERALL_THRESHOLD=80

# --- Colors (disable if not a terminal) ---
if [ -t 1 ]; then
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    BOLD='\033[1m'
    RESET='\033[0m'
else
    RED=''
    GREEN=''
    BOLD=''
    RESET=''
fi

# --- Run tests with coverage ---
COVERFILE=$(mktemp /tmp/mammoth-coverage.XXXXXX)
TESTOUT=$(mktemp /tmp/mammoth-testout.XXXXXX)
trap 'rm -f "$COVERFILE" "$TESTOUT"' EXIT

printf "${BOLD}Running tests with coverage...${RESET}\n"
echo ""

# Allow go test to fail (some packages may not compile) while still capturing output.
go test ./... -coverprofile="$COVERFILE" 2>&1 | tee "$TESTOUT" || true

# If the coverfile is empty, tests failed hard
if [ ! -s "$COVERFILE" ]; then
    printf "${RED}FAIL: No coverage data produced. Tests may have failed to compile.${RESET}\n"
    exit 1
fi

echo ""
printf "${BOLD}Coverage Summary${RESET}\n"
echo "------------------------------------------------------"
printf "%-25s %10s %10s %8s\n" "PACKAGE" "COVERAGE" "THRESHOLD" "STATUS"
echo "------------------------------------------------------"

FAILED=0

# Extract per-package coverage from go test output lines like:
#   ok  	github.com/2389-research/mammoth/agent	2.739s	coverage: 82.3% of statements
# Also handles cached results:
#   ok  	github.com/2389-research/mammoth/llm	(cached)	coverage: 82.1% of statements
extract_coverage() {
    local full_pkg="$1"
    # grep for the "ok" line for this exact package (tab-delimited), extract the percentage.
    # Use awk to match the exact package name in field 2 to avoid partial matches
    # (e.g., "llm" must not match "llm/sse").
    awk -v pkg="$full_pkg" '$1 == "ok" && $2 == pkg' "$TESTOUT" | sed 's/.*coverage: //' | sed 's/% of statements.*//' | head -1
}

for pkg in $PACKAGES; do
    threshold=$(threshold_for "$pkg")
    full_pkg="${PREFIX}/${pkg}"

    pkg_coverage=$(extract_coverage "$full_pkg")

    if [ -z "$pkg_coverage" ]; then
        printf "%-25s %10s %9s%% %8s\n" "$pkg" "N/A" "$threshold" "SKIP"
        continue
    fi

    passed=$(awk "BEGIN {print ($pkg_coverage >= $threshold) ? 1 : 0}")

    if [ "$passed" -eq 1 ]; then
        printf "%-25s %9s%% %9s%% ${GREEN}PASS${RESET}\n" "$pkg" "$pkg_coverage" "$threshold"
    else
        printf "%-25s %9s%% %9s%% ${RED}FAIL${RESET}\n" "$pkg" "$pkg_coverage" "$threshold"
        FAILED=1
    fi
done

echo "------------------------------------------------------"

# Overall total from go tool cover
total_coverage=$(go tool cover -func="$COVERFILE" 2>/dev/null | grep "^total:" | awk '{print $NF}' | tr -d '%')
if [ -n "$total_coverage" ]; then
    passed=$(awk "BEGIN {print ($total_coverage >= $OVERALL_THRESHOLD) ? 1 : 0}")
    if [ "$passed" -eq 1 ]; then
        printf "%-25s %9s%% %9s%% ${GREEN}PASS${RESET}\n" "TOTAL" "$total_coverage" "$OVERALL_THRESHOLD"
    else
        printf "%-25s %9s%% %9s%% ${RED}FAIL${RESET}\n" "TOTAL" "$total_coverage" "$OVERALL_THRESHOLD"
        FAILED=1
    fi
fi

echo "------------------------------------------------------"
echo ""

if [ "$FAILED" -eq 1 ]; then
    printf "${RED}${BOLD}COVERAGE CHECK FAILED${RESET}\n"
    echo "One or more packages are below their coverage threshold."
    echo "See docs/coverage.md for details on critical paths and priorities."
    exit 1
else
    printf "${GREEN}${BOLD}COVERAGE CHECK PASSED${RESET}\n"
    exit 0
fi
