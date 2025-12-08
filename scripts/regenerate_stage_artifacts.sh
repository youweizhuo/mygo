#!/usr/bin/env bash
# Regenerate textual artifacts (SSA/IR/MLIR/SV) for stage workloads.
# Usage: ./scripts/regenerate_stage_artifacts.sh [case ...]
# When no cases are provided it discovers all tests/stages/*/main.go.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
GOCACHE="${GOCACHE:-${ROOT}/.gocache}"
FIFO_SRC="${FIFO_SRC:-${ROOT}/internal/backend/templates/simple_fifo.sv}"
LOWER_OPTS="${LOWER_OPTS:-locationInfoStyle=none,omitVersionComment}"
EMITS="${EMITS:-ssa ir mlir sv}"

mkdir -p "${GOCACHE}"
export GOCACHE

discover_cases() {
	find "${ROOT}/tests/stages" -maxdepth 2 -name main.go -print \
		| sed "s#${ROOT}/tests/stages/##" \
		| sed 's#/main.go##' \
		| sort
}

needs_fifo() {
	local src="$1"
	grep -q 'chan ' "${src}"
}

run_case() {
    local case="$1"
    local src="${ROOT}/tests/stages/${case}/main.go"
    if [[ ! -f "${src}" ]]; then
        echo "skip ${case}: missing ${src}" >&2
        return
    fi
    local dir
    dir="$(dirname "${src}")"
    local extra=()
    if needs_fifo "${src}"; then
        extra+=(--fifo-src "${FIFO_SRC}")
    fi
    for kind in ${EMITS}; do
        echo "[generate] ${case} -> ${kind}"
		case "${kind}" in
			ssa)
				go run ./cmd/mygo compile -emit=ssa -o "${dir}/main.ssa" "${src}"
				;;
			ir)
				go run ./cmd/mygo compile -emit=ir -o "${dir}/main.ir" "${src}"
				;;
			mlir)
				go run ./cmd/mygo compile -emit=mlir -o "${dir}/main.mlir" "${src}"
				;;
			sv|verilog)
			go run ./cmd/mygo compile \
				-emit=verilog \
				--circt-lowering-options="${LOWER_OPTS}" \
				${extra:+"${extra[@]}"} \
				-o "${dir}/main.sv" \
				"${src}"
				;;
			*)
				echo "unknown emit ${kind}, skipping" >&2
				;;
		esac
	done
}

cases=("$@")
if [[ ${#cases[@]} -eq 0 ]]; then
	while IFS= read -r c; do
		cases+=("$c")
	done < <(discover_cases)
fi

for case in "${cases[@]}"; do
	run_case "${case}"
done
