import collections
import pathlib
import re
import sys


def complete_calls(lines):
    """Join interleaved strace records by thread, preserving syscall entry order."""
    records, pending = [], {}
    for line in lines:
        resumed = re.match(r'\s*(\d+)\s+<\.\.\. (\w+) resumed>(.*)', line)
        if resumed:
            pid, call, tail = resumed.groups()
            assert pid in pending, line
            index, expected = pending.pop(pid)
            assert call == expected, line
            records[index] += tail
            assert not records[index].endswith('<unfinished ...>'), records[index]
            continue
        unfinished = re.match(r'\s*(\d+)\s+(\w+)\(.*<unfinished \.\.\.>$', line)
        if unfinished:
            pid, call = unfinished.groups()
            assert pid not in pending, line
            pending[pid] = (len(records), call)
            line = line.removesuffix(' <unfinished ...>')
        records.append(line)
    # A runtime poll can remain blocked when the process exits. Other missing
    # completions mean incomplete evidence and must fail closed.
    for index, call in pending.values():
        assert call in {'epoll_pwait', 'epoll_wait', 'epoll_pwait2'}, records[index]
    return records


def analyze(lines):
    windows = {}
    active = None
    for line in complete_calls(lines):
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
            else:
                assert '--- SIG' in line or '+++ exited' in line, line
        rows.append(f'{name}: observed={dict(calls)}')
        assert not (set(calls) - {'epoll_pwait', 'epoll_wait', 'epoll_pwait2', 'mmap_anonymous'}), (name, calls)
    rows.append('PASS: no file access, descriptor reads/writes or network calls in Evaluate windows; marker writes excluded.')
    return '\n'.join(rows) + '\n'


if __name__ == "__main__":
    root = pathlib.Path(sys.argv[1])
    text = analyze((root / "syscalls.txt").read_text().splitlines())
    (root / "io-summary.txt").write_text(text)
    print(text, end="")
