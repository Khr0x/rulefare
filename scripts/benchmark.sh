#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
output=${1:-bin/benchmarks}
mode=${2:-local}
if [[ "$mode" != local && "$mode" != --container ]]; then
  printf 'Usage: bash scripts/benchmark.sh [output-directory] [--container]\n' >&2
  exit 2
fi
mkdir -p "$output"
output=$(cd "$output" && pwd)
export GOMAXPROCS=${GOMAXPROCS:-1}
if [[ "$mode" == --container ]]; then
  # Leave headroom for stacks, executable pages and non-Go allocations below
  # the container's hard 512 MiB cap. No image or package downloads are needed.
  export GOMEMLIMIT=${GOMEMLIMIT:-448MiB}
  target_arch=$(docker info --format '{{.Architecture}}')
  case "$target_arch" in
    aarch64|arm64) target_arch=arm64 ;;
    x86_64|amd64) target_arch=amd64 ;;
    *) printf 'Unsupported Docker architecture: %s\n' "$target_arch" >&2; exit 1 ;;
  esac
  mkdir -p "$output/image"
  GOOS=linux GOARCH="$target_arch" CGO_ENABLED=0 go test -c -o "$output/image/engine.test" ./engine
  cp -R engine/testdata "$output/image/"
  cat > "$output/image/Dockerfile" <<'DOCKERFILE'
FROM scratch
COPY engine.test /engine.test
COPY testdata /testdata
ENTRYPOINT ["/engine.test"]
DOCKERFILE
  docker build --network=none -t rulefare-benchmark:local "$output/image"
  image_id=$(docker image inspect rulefare-benchmark:local --format '{{.Id}}')
else
  export GOMEMLIMIT=${GOMEMLIMIT:-512MiB}
  go test -c -o "$output/engine.test" ./engine
fi

{
  printf 'UTC: %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  printf 'Commit: %s\n' "$(git rev-parse HEAD)"
  printf 'GOMAXPROCS=%s\nGOMEMLIMIT=%s\n' "$GOMAXPROCS" "$GOMEMLIMIT"
  printf 'Mode: %s\n' "$mode"
  if [[ "$mode" == --container ]]; then
    printf 'Target: linux/%s; CPU quota 1; memory 512 MiB; swap disabled; network disabled\n' "$target_arch"
    printf 'Image: %s\n' "$image_id"
    docker version --format 'Docker server: {{.Server.Version}}'
  else
    printf 'Controls: GOMAXPROCS is not a CPU quota; GOMEMLIMIT is a soft Go runtime limit.\n'
  fi
  go version
  go env GOOS GOARCH
  go list -m github.com/google/cel-go
  uname -a
  printf 'Working tree (empty means clean):\n'
  git status --short
  printf 'Source and fixture SHA256:\n'
  if command -v sha256sum >/dev/null; then
    sha256sum go.mod go.sum engine/*.go engine/testdata/*.json scripts/benchmark.sh
  else
    shasum -a 256 go.mod go.sum engine/*.go engine/testdata/*.json scripts/benchmark.sh
  fi
} > "$output/environment.txt"

run_benchmark() {
  if [[ "$mode" == --container ]]; then
    docker run --rm --network=none --read-only --cpus=1 --memory=512m --memory-swap=512m \
      -e GOMAXPROCS -e GOMEMLIMIT "$image_id" "$@"
  else
    "$output/engine.test" "$@"
  fi
}

cd engine
run_benchmark -test.run '^$' -test.bench '^Benchmark(Evaluate|EvaluateUnique|Compile10000|CompileUnique10000)$' \
  -test.benchmem -test.benchtime=1s -test.count=3 | tee "$output/throughput.txt"
run_benchmark -test.run '^$' -test.bench '^BenchmarkEvaluateLatency100$' \
  -test.benchtime=10000x -test.count=3 | tee "$output/latency.txt"
# Fresh process: prior benchmark compilations must not inflate resident RSS.
run_benchmark -test.run '^$' -test.bench '^BenchmarkProgramResident10000$' \
  -test.benchtime=1x -test.count=1 | tee "$output/resident.txt"
# Three fresh processes for the unique-condition control; count=3 in one
# process would keep runtime/library state from the previous compilation.
for repetition in 1 2 3; do
  run_benchmark -test.run '^$' -test.bench '^BenchmarkProgramResidentUnique10000$' \
    -test.benchtime=1x -test.count=1
done | tee "$output/unique-resident.txt"
run_benchmark -test.run '^$' -test.bench '^BenchmarkResourceLimits$' \
  -test.benchmem -test.benchtime=1s -test.count=3 | tee "$output/limits.txt"
