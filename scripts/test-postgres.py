#!/usr/bin/env python3
"""Disposable local PostgreSQL integration fixture; never uses an existing cluster."""
import os
import pathlib
import secrets
import shutil
import socket
import subprocess
import tempfile

root = pathlib.Path(tempfile.mkdtemp(prefix="tgstrm-pg-"))
secret = secrets.token_urlsafe(32)
pwfile = root / "password"
pwfile.write_text(secret)
pwfile.chmod(0o600)
started = False
try:
    subprocess.run(["initdb", "-D", str(root / "cluster"), "-U", "tgstrm_test", "--auth=scram-sha-256", "--pwfile=" + str(pwfile)], check=True, stdout=subprocess.DEVNULL)
    with socket.socket() as probe:
        probe.bind(("127.0.0.1", 0))
        port = probe.getsockname()[1]
    subprocess.run(["pg_ctl", "-D", str(root / "cluster"), "-l", str(root / "postgres.log"), "-o", f"-h 127.0.0.1 -p {port} -k {root}", "-w", "start"], check=True, stdout=subprocess.DEVNULL)
    started = True
    env = os.environ.copy()
    env.update(TGSTRM_TEST_PG_PASSWORD=secret, TGSTRM_TEST_PG_PORT=str(port))
    subprocess.run(["go", "test", "-race", "-count=1", "./..."], env=env, check=True)
finally:
    if started:
        subprocess.run(["pg_ctl", "-D", str(root / "cluster"), "-m", "fast", "-w", "stop"], check=True, stdout=subprocess.DEVNULL)
    shutil.rmtree(root)
