#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
output=${1:-bin/system}
mkdir -p "$output/image"
output=$(cd "$output" && pwd)
arch=$(docker info --format '{{.Architecture}}')
case "$arch" in
  aarch64|arm64) arch=arm64 ;;
  x86_64|amd64) arch=amd64 ;;
  *) printf 'Unsupported Docker architecture: %s\n' "$arch" >&2; exit 1 ;;
esac
GOOS=linux GOARCH="$arch" CGO_ENABLED=0 go test -c -o "$output/image/engine.test" ./engine
cp -R engine/testdata "$output/image/"
cat > "$output/image/Dockerfile" <<'DOCKERFILE'
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends strace && rm -rf /var/lib/apt/lists/*
COPY engine.test /engine.test
COPY testdata /testdata
ENTRYPOINT ["/engine.test"]
DOCKERFILE
# Only image preparation uses the network. Measured containers cannot use it.
docker build -t rulefare-system:local "$output/image"
image_id=$(docker image inspect rulefare-system:local --format '{{.Id}}')
options=(--rm --network=none --read-only --cpus=1 --memory=512m --memory-swap=512m
  -e GOMAXPROCS=1 -e GOMEMLIMIT=448MiB -e RULEFARE_SYSTEM=1)
{
  date -u +%Y-%m-%dT%H:%M:%SZ
  printf 'Commit: %s\nImage: %s\n' "$(git rev-parse HEAD)" "$image_id"
  printf 'linux/%s; 1 CPU; 512 MiB; no swap/network; GOMAXPROCS=1; GOMEMLIMIT=448MiB\n' "$arch"
  go version
  go list -m github.com/google/cel-go
  uname -a
  docker version --format 'Docker server: {{.Server.Version}}'
  docker run "${options[@]}" --entrypoint uname "$image_id" -a
  docker run "${options[@]}" --entrypoint strace "$image_id" -V
  git status --short
  shasum -a 256 go.mod go.sum engine/*.go engine/testdata/*.json scripts/measure-system.sh "$output/image/engine.test"
} > "$output/environment.txt"
for repetition in 1 2 3; do
  docker run "${options[@]}" "$image_id" -test.run '^TestSystemIdle$' -test.v -test.timeout=30s
done | tee "$output/idle.txt"
# Trace all threads and file/network/descriptor syscalls, including failed
# attempts. The writable mount stores only the observer's raw evidence.
docker run "${options[@]}" --cap-add=SYS_PTRACE \
  -v "$output:/evidence" --entrypoint strace "$image_id" \
  -f -qq -s 256 -e trace=%file,%network,%desc -o /evidence/syscalls.txt \
  /engine.test -test.run '^TestSystemEvaluationIO$' -test.v -test.timeout=2m \
  2>&1 | tee "$output/evaluation.txt"
python3 - "$output" <<'PY'
import collections, pathlib, re, sys
root = pathlib.Path(sys.argv[1])
windows = {}
active = None
for line in (root / 'syscalls.txt').read_text().splitlines():
    marker = re.search(r'write\(2, "RULEFARE_IO_(BEGIN|END) (\w+)\\n"', line)
    if marker:
        kind, name = marker.groups()
        if kind == 'BEGIN':
            assert active is None and name not in windows, line
            active = name
            windows[name] = []
        else:
            assert active == name, line
            active = None
        continue
    if active is not None:
        windows[active].append(line)
assert active is None
assert set(windows) == {'control', 'repeated100', 'unique100', 'unique10000', 'invalid', 'no_match'}
control = '\n'.join(windows['control'])
assert re.search(r'openat\(.*testdata/schema.json', control), 'file positive control absent'
assert 'socket(AF_INET' in control, 'network positive control absent'
rows = ['Positive control: file open and network socket observed.']
for name, lines in windows.items():
    if name == 'control':
        continue
    # Signals and exit notices are not I/O syscalls. Runtime readiness waits
    # may appear; retain them in the report rather than attributing them to disk.
    calls = collections.Counter()
    for line in lines:
        match = re.match(r'\s*\d+\s+(\w+)\(', line)
        if match:
            name_of_call = match[1]
            if name_of_call == 'mmap':
                assert 'MAP_ANONYMOUS' in line and re.search(r', -1, 0\)', line), line
                name_of_call = 'mmap_anonymous'
            calls[name_of_call] += 1
        elif '<... ' in line and ' resumed>' in line:
            # Count on entry, not again on completion.
            continue
        else:
            assert '--- SIG' in line or '+++ exited' in line, line
    rows.append(f'{name}: observed={dict(calls)}')
    assert not (set(calls) - {'epoll_pwait', 'epoll_wait', 'epoll_pwait2', 'mmap_anonymous'}), (name, calls)
rows.append('PASS: no file access, descriptor reads/writes or network calls in Evaluate windows; marker writes excluded.')
text = '\n'.join(rows) + '\n'
(root / 'io-summary.txt').write_text(text)
print(text, end='')
PY
