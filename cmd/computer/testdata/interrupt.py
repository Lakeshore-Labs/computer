"""Exercise the interactive CLI in a controlling pseudo-terminal."""
import errno
import fcntl
import os
import pty
import select
import struct
import sys
import termios
import time

binary, directory = sys.argv[1:]
pid, terminal = pty.fork()
if pid == 0:
    fcntl.ioctl(0, termios.TIOCSWINSZ, struct.pack('HHHH', 24, 100, 0, 0))
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
finally:
    os.close(terminal)
    if pid is not None:
        try:
            os.kill(pid, 9)
        except ProcessLookupError:
            pass
        os.waitpid(pid, 0)
