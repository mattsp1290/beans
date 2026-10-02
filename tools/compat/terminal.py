"""POSIX pseudo-terminal capture with a fixed size and bounded process lifetime."""
import errno
import os
import select
import struct
import subprocess
import tempfile
import time


def capture(argv, *, env, cwd, stdin, timeout):
    import fcntl
    import pty
    import termios

    readers, writers = [], []
    process = None
    input_file = tempfile.TemporaryFile()
    try:
        input_file.write(stdin)
        input_file.seek(0)
        for _ in range(2):
            master, slave = pty.openpty()
            fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 24, 80, 0, 0))
            readers.append(master)
            writers.append(slave)
        process = subprocess.Popen(argv, env=env, cwd=cwd, stdin=input_file,
                                   stdout=writers[0], stderr=writers[1])
        for fd in writers:
            os.close(fd)
        writers.clear()
        buffers = {fd: bytearray() for fd in readers}
        active = set(readers)
        deadline = time.monotonic() + timeout
        while active:
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise subprocess.TimeoutExpired(argv, timeout)
            ready, _, _ = select.select(list(active), [], [], min(remaining, 0.1))
            for fd in ready:
                try:
                    data = os.read(fd, 65536)
                except OSError as error:
                    if error.errno != errno.EIO:
                        raise
                    data = b""
                if data:
                    buffers[fd].extend(data)
                else:
                    active.remove(fd)
        code = process.wait(timeout=max(0.001, deadline - time.monotonic()))
        return subprocess.CompletedProcess(argv, code, bytes(buffers[readers[0]]), bytes(buffers[readers[1]]))
    finally:
        if process is not None and process.poll() is None:
            process.kill()
            process.wait()
        for fd in readers + writers:
            os.close(fd)
        input_file.close()
