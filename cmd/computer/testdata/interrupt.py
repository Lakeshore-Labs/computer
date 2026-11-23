"""Exercise the interactive CLI in a controlling pseudo-terminal."""
import errno
import fcntl
import os
import pty
from pathlib import Path
import select
import struct
import sys
import termios
import time

binary, directory, mode = sys.argv[1:]
pid, terminal = pty.fork()
if pid == 0:
    fcntl.ioctl(0, termios.TIOCSWINSZ, struct.pack('HHHH', 24, 100, 0, 0))
    if mode == 'nonblocking':
        os.set_blocking(0, False)
    if mode == 'inherited-only':
        Path(directory, 'tty-mode').write_text(f'{os.fstat(0).st_mode & 0o7777} {os.ttyname(0)}')
        os.fchmod(0, 0)
    os.execv(binary, [binary, '-mount', directory + ':/work'])

pending = bytearray()

def expect(marker):
    deadline = time.monotonic() + 8
    while marker not in pending:
        remaining = deadline - time.monotonic()
        if remaining <= 0 or not select.select([terminal], [], [], remaining)[0]:
            raise AssertionError(f'Timed out waiting for {marker!r}: {pending!r}')
        try:
            chunk = os.read(terminal, 4096)
        except OSError as exc:
            if exc.errno != errno.EIO:
                raise
            chunk = b''
        if not chunk:
            raise AssertionError(f'Prompt exited before {marker!r}: {pending!r}')
        pending.extend(chunk)
    del pending[:pending.index(marker) + len(marker)]

try:
    expect(b'$ ')
    if mode == 'nonblocking':
        os.write(terminal, b'read -t 0.1 x; echo timed-out\r')
        expect(b'\r\ntimed-out\r\n')
        expect(b'$ ')
    os.write(terminal, b'echo waiting; read line\r')
    expect(b'\r\nwaiting\r\n')
    time.sleep(0.05)
    os.write(terminal, b'\x03')
    expect(b'$ ')
    os.write(terminal, b'echo taking-one; read -n 1 x; echo took-one; sleep 30\r')
    expect(b'\r\ntaking-one\r\n')
    os.write(terminal, b'xecho leftover > /work/unexpected.txt\n')
    expect(b'took-one\r\n')
    os.write(terminal, b'\x03')
    expect(b'$ ')
    os.write(terminal, b"echo ready; sleep 30\r")
    expect(b'\r\nready\r\n')
    os.write(terminal, b'\x03')
    expect(b'$ ')
    # cat reads directly; pr copies stdin with Go's io.Copy.
    for command, output in [(b'cat -n', b'1\tfirst line\r\n'), (b'pr', b'first line\r\n' * 2)]:
        os.write(terminal, b'echo reading; ' + command + b'\r')
        expect(b'\r\nreading\r\n')
        os.write(terminal, b'first line\n')
        expect(output)
        os.write(terminal, b'\x03')
        expect(b'$ ')
    os.write(terminal, b'echo resumed\r')
    expect(b'\r\nresumed\r\n')
    expect(b'$ ')
    os.write(terminal, b'exit\r')
    _, status = os.waitpid(pid, 0)
    pid = None
    assert os.waitstatus_to_exitcode(status) == 0
    assert not os.path.exists(os.path.join(directory, 'unexpected.txt')), 'canceled input ran as a command'
finally:
    if mode == 'inherited-only':
        original_mode, device = Path(directory, 'tty-mode').read_text().split(' ', 1)
        os.chmod(device, int(original_mode))
    os.close(terminal)
    if pid is not None:
        try:
            os.kill(pid, 9)
        except ProcessLookupError:
            pass
        os.waitpid(pid, 0)
