import unittest

from analyze_syscalls import analyze, complete_calls


def marker(kind, name):
    return f'10 write(2, "RULEFARE_IO_{kind} {name}\\n", 40) = 40'


def trace(events):
    lines = [marker('BEGIN', 'control'),
             '10 openat(AT_FDCWD, "testdata/schema.json", O_RDONLY) = 3',
             '10 socket(AF_INET, SOCK_STREAM, 0) = 4', marker('END', 'control')]
    for name in ('repeated100', 'unique100', 'unique10000', 'invalid', 'no_match'):
        lines.append(marker('BEGIN', name))
        if name == 'repeated100':
            lines.extend(events)
        lines.append(marker('END', name))
    return lines


class SyscallTraceTests(unittest.TestCase):
    def test_reported_mmap_and_interleaved_threads(self):
        events = [
            '11 mmap(0x267e64800000, 4194304, PROT_READ|PROT_WRITE, MAP_PRIVATE|MAP_FIXED|MAP_ANONYMOUS, -1, 0 <unfinished ...>',
            '12 epoll_pwait(3,  <unfinished ...>',
            '11 <... mmap resumed>) = 0x267e64800000',
            '12 <... epoll_pwait resumed>[], 128, 0, NULL, 0) = 0',
        ]
        report = analyze(trace(events))
        self.assertIn("'mmap_anonymous': 1", report)
        self.assertIn("'epoll_pwait': 1", report)

    def test_split_arguments_and_markers(self):
        lines = trace(['11 mmap(NULL, 4096, PROT_READ, MAP_PRIVATE|MAP_ANONYMOUS, <unfinished ...>',
                       '11 <... mmap resumed> -1, 0) = 0xffff0000'])
        lines[0:1] = ['10 write(2, <unfinished ...>',
                      '10 <... write resumed> "RULEFARE_IO_BEGIN control\\n", 40) = 40']
        self.assertIn("'mmap_anonymous': 1", analyze(lines))

    def test_real_io_is_rejected_even_when_split(self):
        for entry, resumed in [
            ('openat(AT_FDCWD, "/tmp/file", O_RDONLY', 'openat resumed>) = 3'),
            ('socket(AF_INET, SOCK_STREAM, 0', 'socket resumed>) = 4'),
            ('write(4, "data", 4', 'write resumed>) = 4'),
            ('mmap(NULL, 4096, PROT_READ, MAP_PRIVATE, 4, 0', 'mmap resumed>) = 0xffff0000'),
            ('mmap(NULL, 4096, PROT_READ, MAP_PRIVATE|MAP_ANONYMOUS, 4, 0', 'mmap resumed>) = 0xffff0000'),
        ]:
            with self.subTest(entry=entry), self.assertRaises(AssertionError):
                analyze(trace([f'11 {entry} <unfinished ...>', f'11 <... {resumed}']))

    def test_entry_order_preserved_across_end_marker(self):
        lines = trace(['11 write(4, "data", 4 <unfinished ...>'])
        lines.append('11 <... write resumed>) = 4')
        with self.assertRaises(AssertionError):
            analyze(lines)

    def test_incomplete_evidence_fails_except_runtime_poll(self):
        for lines in [
            ['11 mmap(NULL, 4096, <unfinished ...>'],
            ['11 <... mmap resumed>) = 0xffff0000'],
            ['11 mmap(NULL, <unfinished ...>', '11 <... write resumed>) = 1'],
        ]:
            with self.subTest(lines=lines), self.assertRaises(AssertionError):
                complete_calls(lines)
        self.assertIn('PASS:', analyze(trace(['12 epoll_pwait(3, <unfinished ...>'])))

    def test_positive_controls_required(self):
        with self.assertRaises(AssertionError):
            analyze([line for line in trace([]) if 'socket(AF_INET' not in line])


if __name__ == '__main__':
    unittest.main()
