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
    init_db = "tgstrm_init_script_test"
    init_env = env.copy()
    init_env.update(PGPASSWORD=secret, PGHOST="127.0.0.1", PGPORT=str(port))
    subprocess.run(["createdb", "-h", "127.0.0.1", "-p", str(port), "-U", "tgstrm_test", init_db], env=init_env, check=True)
    app_password = secrets.token_urlsafe(32)
    init_env.update(POSTGRES_DB=init_db, POSTGRES_USER="tgstrm_test", POSTGRES_PASSWORD=secret, TGSTRM_DATABASE_PASSWORD=app_password)
    subprocess.run(["sh", "deploy/postgres-init.sh"], env=init_env, check=True)
    owner = subprocess.check_output(["psql", "-h", "127.0.0.1", "-p", str(port), "-U", "tgstrm_test", "-d", "postgres", "-tA", "-c", f"SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname='{init_db}'"], env=init_env, text=True).strip()
    role = subprocess.check_output(["psql", "-h", "127.0.0.1", "-p", str(port), "-U", "tgstrm_test", "-d", "postgres", "-tA", "-c", "SELECT rolsuper::text || ':' || rolcreatedb::text FROM pg_roles WHERE rolname='tgstrm'"], env=init_env, text=True).strip()
    if owner != "tgstrm" or role != "false:false":
        raise RuntimeError("PostgreSQL init script did not create a least-privilege owner role")
    app_env = init_env.copy()
    app_env["PGPASSWORD"] = app_password
    subprocess.run(["psql", "-h", "127.0.0.1", "-p", str(port), "-U", "tgstrm", "-d", init_db, "-v", "ON_ERROR_STOP=1", "-c", "SELECT 1"], env=app_env, check=True, stdout=subprocess.DEVNULL)
    subprocess.run(["go", "test", "-count=1", "./..."], env=env, check=True)
    subprocess.run(["go", "test", "-race", "-count=1", "./..."], env=env, check=True)
finally:
    if started:
        subprocess.run(["pg_ctl", "-D", str(root / "cluster"), "-m", "fast", "-w", "stop"], check=True, stdout=subprocess.DEVNULL)
    shutil.rmtree(root)
