"""Exercise the interactive CLI in a controlling pseudo-terminal.

The CLI runs under sh, which stays the terminal's session leader as a login shell would. Once the CLI ends, the test
checks that the terminal is as sh gave it: in blocking or non-blocking mode as it started, and with its line settings
back. A mode named sigterm ends the CLI with SIGTERM instead of exit."""
import errno
import fcntl
import os
import pty
import re
import select
import signal
import struct
import subprocess
import sys
import termios
import time

binary, directory, mode = sys.argv[1:]
terminal, tty = pty.openpty()
fcntl.ioctl(tty, termios.TIOCSWINSZ, struct.pack('HHHH', 24, 100, 0, 0))
if mode == 'nonblocking':
    os.set_blocking(tty, False)
started_blocking, settings = os.get_blocking(tty), termios.tcgetattr(tty)
device, permissions = os.ttyname(tty), os.fstat(tty).st_mode & 0o7777

def controlling_terminal():
    os.setsid()
    fcntl.ioctl(0, termios.TIOCSCTTY, 0)
    if mode == 'inherited-only':
        os.fchmod(0, 0)  # the CLI can use the descriptor it was given, but cannot open the device again

command = [binary, '-mount', directory + ':/work']
python = os.environ.get('PYTHON_WASM'), os.environ.get('PYTHON_LIB')
if all(python):
    command += ['-python', python[0], '-python-lib', python[1]]
# sh survives Ctrl-C and SIGTERM, which are the CLI's to handle, and waits after the CLI ends so the terminal stays.
shell = 'trap : INT TERM; "$@"; echo "cli-exit=$?"; sleep 30'
session = subprocess.Popen(['sh', '-c', shell, 'sh', *command], stdin=tty, stdout=tty, stderr=tty,
                           preexec_fn=controlling_terminal)

pending, last = bytearray(), bytearray()

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
    global last
    last = pending[:pending.index(marker)]
    del pending[:pending.index(marker) + len(marker)]

try:
    expect(b'$ ')
    os.write(terminal, b'read -t 0.1 x; echo timed-out $?\r')
    expect(b'\r\ntimed-out 142\r\n')
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
    if all(python):
        # A program that waits for input and gives up leaves its read of the terminal under way. Ending the command
        # must end that read too, or it takes the next line typed at the prompt.
        os.write(terminal, b'python3 -c "import select, sys; select.select([sys.stdin], [], [], 0.2)"; echo gave-up\r')
        expect(b'\r\ngave-up\r\n')
        expect(b'$ ')
    os.write(terminal, b'echo resumed\r')
    expect(b'\r\nresumed\r\n')
    expect(b'$ ')
    if mode == 'sigterm':
        os.killpg(session.pid, signal.SIGTERM)
    else:
        os.write(terminal, b'exit\r')
    expect(b'cli-exit=')
    expect(b'\r\n')
    status = int(re.findall(rb'\d+', bytes(last))[-1])
    assert status == 0, f'exit status {status}'
    assert not os.path.exists(os.path.join(directory, 'unexpected.txt')), 'canceled input ran as a command'
    assert os.get_blocking(tty) == started_blocking, f'the terminal was left with blocking={os.get_blocking(tty)}'
    def line_settings(attributes):  # PENDIN is the kernel's note that input is waiting, not a setting
        return [*attributes[:3], attributes[3] & ~termios.PENDIN, *attributes[4:]]
    assert line_settings(termios.tcgetattr(tty)) == line_settings(settings), 'the terminal was left in raw mode'
finally:
    os.chmod(device, permissions)
    os.close(terminal)  # first, so that nothing waits for its output to be read
    session.kill()
    session.wait()
    os.close(tty)
