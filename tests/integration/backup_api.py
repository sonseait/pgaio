#!/usr/bin/env python3
"""Exercise current checkout's admin backup API against an isolated Docker stack.

Requires Go, Docker, and a PGAIO runtime image containing PostgreSQL and WAL-G.
Example: python3 tests/integration/backup_api.py --image pgaio:layer-size-check
The current Go binary and entrypoint scripts are built/copied and mounted; the
runtime image supplies dependencies. Only temporary test data is restored.
"""

import argparse
import datetime as dt
import json
import os
from pathlib import Path
import secrets
import shutil
import subprocess
import tempfile
import time
import urllib.error
import urllib.request


REPO = Path(__file__).resolve().parents[2]


class Suite:
    def __init__(self, image, output):
        self.image = image
        self.output = output
        self.prefix = "pgaio-backup-test-" + secrets.token_hex(4)
        self.db = self.prefix + "-db"
        self.silo = self.prefix + "-silo"
        self.volume = self.prefix + "-data"
        self.password = secrets.token_hex(16)
        self.session = ""
        self.base = ""
        self.results = {}

    def command(self, *args, check=True, **kwargs):
        result = subprocess.run(args, capture_output=True, text=True, **kwargs)
        if check and result.returncode:
            raise AssertionError("Command failed: " + result.stdout + result.stderr)
        return result

    def docker(self, *args, **kwargs):
        return self.command("docker", *args, **kwargs)

    def api(self, path, body=None, authenticated=True):
        headers = {"Content-Type": "application/json"}
        if authenticated:
            headers["X-Session-ID"] = self.session
        request = urllib.request.Request(
            self.base + path,
            data=None if body is None else json.dumps(body).encode(),
            headers=headers,
        )
        try:
            response = urllib.request.urlopen(request, timeout=30)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            status, result = response.code, json.load(response)
        if path != "/api/auth/login":
            with (self.output / "api.jsonl").open("a") as stream:
                stream.write(json.dumps({"path": path, "request": body,
                                         "http": status, "response": result}) + "\n")
        return status, result

    def data(self, path, body=None):
        status, result = self.api(path, body)
        assert result.get("success"), (status, result)
        return result["data"]

    def sql(self, query):
        return self.data("/api/sql/execute", {"query": query})["rows"]

    def job(self, path, body, expected="succeeded"):
        response = self.data(path, body)
        deadline = time.monotonic() + 330
        while time.monotonic() < deadline:
            job = self.data("/api/jobs/" + response["jobId"])
            if job["status"] != "running":
                assert job["status"] == expected, job
                self.results.setdefault("jobs", []).append(job)
                print(job["status"].upper(), job["type"], job["message"], flush=True)
                return job
            time.sleep(0.25)
        raise TimeoutError(response)

    def snapshot(self, where=""):
        return self.sql("SELECT count(*) AS count, "
                        "md5(string_agg(id::text || ':' || value, ',' ORDER BY id)) AS checksum "
                        "FROM public.backup_api_probe " + where)

    def archive(self):
        # Force a WAL record even on an idle cluster. Nontransactional logical
        # messages do not add a commit that could satisfy a time target.
        wal = self.sql("SELECT pg_walfile_name(pg_logical_emit_message(false, "
                       "'pgaio-integration', 'archive')) AS wal")[0]["wal"]
        self.sql("SELECT pg_switch_wal()")
        deadline = time.monotonic() + 60
        while time.monotonic() < deadline:
            state = self.sql("SELECT last_archived_wal, failed_count FROM pg_stat_archiver")[0]
            if state["last_archived_wal"] and state["last_archived_wal"] >= wal:
                assert state["failed_count"] == 0, state
                return
            time.sleep(0.25)
        raise TimeoutError("WAL was not archived")

    def start(self, work):
        env = dict(os.environ, CGO_ENABLED="0", GOOS="linux", GOARCH="amd64")
        self.command("go", "build", "-o", str(work / "pgaio"), ".", cwd=REPO / "backend", env=env)
        for source in [REPO / "docker-entrypoint.sh", REPO / "postgresql/docker-entrypoint-pg.sh"]:
            shutil.copyfile(source, work / source.name)
            (work / source.name).chmod(0o755)
        self.docker("network", "create", self.prefix)
        self.docker("run", "-d", "--name", self.silo, "--network", self.prefix,
                    "-e", "MINIO_ROOT_USER=minioadmin", "-e", "MINIO_ROOT_PASSWORD=minioadmin",
                    "pgsty/silo:latest", "server", "/data")
        deadline = time.monotonic() + 30
        while time.monotonic() < deadline:
            result = self.docker("exec", self.silo, "mcli", "alias", "set", "test",
                                 "http://127.0.0.1:9000", "minioadmin", "minioadmin", check=False)
            if result.returncode == 0:
                break
            time.sleep(0.5)
        else:
            raise TimeoutError("Silo did not start")
        self.docker("exec", self.silo, "mcli", "mb", "test/api-test")
        # Mount PGDATA separately from its parent to catch cross-filesystem
        # staging/rename errors. Initialize ownership for the rootless server.
        self.docker("run", "--rm", "--platform", "linux/amd64", "--user", "0",
                    "--entrypoint", "/bin/bash", "-v", self.volume + ":/bitnami/postgresql/data",
                    self.image, "-c", "chown 1001:0 /bitnami/postgresql/data; chmod 700 /bitnami/postgresql/data")
        environment = {
            "ALLOW_EMPTY_PASSWORD": "yes", "POSTGRESQL_PASSWORD": "postgres",
            "PGAIO_PASSWORD": self.password, "WALG_S3_PREFIX": "s3://api-test/wal-g",
            "AWS_ACCESS_KEY_ID": "minioadmin", "AWS_SECRET_ACCESS_KEY": "minioadmin",
            "AWS_ENDPOINT": "http://" + self.silo + ":9000", "AWS_S3_FORCE_PATH_STYLE": "true",
            "AWS_REGION": "us-east-1", "WALG_LIBSODIUM_KEY": secrets.token_hex(32),
        }
        args = ["run", "-d", "--platform", "linux/amd64", "--name", self.db,
                "--network", self.prefix, "-p", "127.0.0.1::8080",
                "-v", self.volume + ":/bitnami/postgresql/data"]
        for key, value in environment.items():
            args += ["-e", key + "=" + value]
        for name, destination in [("pgaio", "/usr/local/bin/pgaio"),
                                  ("docker-entrypoint.sh", "/docker-entrypoint.sh"),
                                  ("docker-entrypoint-pg.sh", "/docker-entrypoint-pg.sh")]:
            args += ["-v", str(work / name) + ":" + destination + ":ro"]
        self.docker(*args, self.image)
        self.base = "http://" + self.docker("port", self.db, "8080/tcp").stdout.strip()
        deadline = time.monotonic() + 90
        while time.monotonic() < deadline:
            try:
                logs = self.docker("logs", self.db)
                # Wait for the one-time extension installer too, so it cannot
                # create commits during the unreachable-target test below.
                if "Extensions ready" not in logs.stdout + logs.stderr:
                    time.sleep(0.5)
                    continue
                _, response = self.api("/api/auth/login", {"password": self.password}, False)
                self.session = response["data"]["sessionId"]
                self.sql("SELECT 1")
                return
            except (OSError, KeyError, AssertionError):
                time.sleep(0.5)
        raise TimeoutError("PGAIO did not become ready")

    def test(self):
        for path in ["/api/backups", "/api/backups/trigger", "/api/backups/restore", "/api/backups/verify"]:
            assert self.api(path, None if path == "/api/backups" else {}, False)[0] == 401
        for body in [{"kind": "invalid"}]:
            assert self.api("/api/backups/trigger", body)[0] == 400
        cfg = self.data("/api/settings")
        cfg["backup"]["enabled"] = False
        self.data("/api/settings", cfg)
        # Keep the unreachable-target test deterministic: no background
        # autovacuum transaction should commit after its target timestamp.
        self.sql("ALTER SYSTEM SET autovacuum = off")
        self.sql("SELECT pg_reload_conf()")
        self.job("/api/backups/trigger", {}, "canceled")
        self.sql("CREATE TABLE public.backup_api_probe (id integer PRIMARY KEY, value text NOT NULL)")
        self.sql("INSERT INTO public.backup_api_probe SELECT i, md5(i::text) FROM generate_series(1,1000) i")
        self.job("/api/backups/trigger", {"kind": "full"})
        full = self.data("/api/backups")["lastBackup"]
        assert "_D_" not in full
        self.sql("UPDATE public.backup_api_probe SET value='delta-one' WHERE id<=100")
        self.sql("DELETE FROM public.backup_api_probe WHERE id>990")
        self.sql("INSERT INTO public.backup_api_probe VALUES (1001,'added-in-delta-one')")
        self.job("/api/backups/trigger", {})  # backwards-compatible empty body
        delta1 = self.data("/api/backups")["lastBackup"]
        assert "_D_" in delta1
        self.sql("UPDATE public.backup_api_probe SET value='delta-two' WHERE id BETWEEN 101 AND 200")
        self.sql("INSERT INTO public.backup_api_probe VALUES (1002,'added-in-delta-two')")
        self.job("/api/backups/trigger", {"kind": "incremental"})
        delta2 = self.data("/api/backups")["lastBackup"]
        assert "_D_" in delta2 and delta2 != delta1
        baseline = self.snapshot()
        assert baseline[0]["count"] == 992
        for name in [full, delta1, delta2]:
            self.job("/api/backups/verify", {"backupName": name})
        self.sql("INSERT INTO public.backup_api_probe VALUES (2000,'before-target')")
        timestamp = self.sql("SELECT to_char(clock_timestamp() AT TIME ZONE 'UTC', "
                             "'YYYY-MM-DD\"T\"HH24:MI:SS.US\"Z\"') AS target")[0]["target"]
        target = dt.datetime.fromisoformat(timestamp.replace("Z", "+00:00")).astimezone(
            dt.timezone(dt.timedelta(hours=4))).isoformat()
        time.sleep(0.5)
        self.sql("INSERT INTO public.backup_api_probe VALUES (2001,'after-target')")
        self.job("/api/backups/trigger", {})
        delta3 = self.data("/api/backups")["lastBackup"]
        self.archive()
        for body in [{}, {"backupName": "missing"},
                     {"backupName": delta3, "targetTime": target},
                     {"backupName": "LATEST", "targetTime": "2000-01-01T00:00:00Z"},
                     {"backupName": "LATEST", "targetTime": "2999-01-01T00:00:00Z"},
                     {"backupName": "LATEST", "targetTime": "x'\narchive_mode=off"}]:
            assert self.api("/api/backups/restore", body)[0] == 400, body
        assert self.snapshot()[0]["count"] == 994  # invalid requests did not stop/delete data
        pitr = self.job("/api/backups/restore", {"backupName": "LATEST", "targetTime": target})
        assert pitr["metadata"]["backupName"] == delta2, pitr
        assert self.sql("SELECT id FROM public.backup_api_probe WHERE id>=2000 ORDER BY id") == [{"id": 2000}]
        assert self.snapshot("WHERE id<2000") == baseline
        assert self.sql("SELECT pg_is_in_recovery() AS recovering") == [{"recovering": False}]
        self.results["pitr"] = {"target": target, "selectedBackup": delta2, "baseline": baseline}
        # Back up a recovered cluster, then restore it without inheriting the
        # old recovery_target_time from postgresql.auto.conf.
        self.job("/api/backups/trigger", {"kind": "full"})
        recovered_full = self.data("/api/backups")["lastBackup"]
        recovered_snapshot = self.snapshot()
        self.sql("INSERT INTO public.backup_api_probe VALUES (3000,'must-be-removed')")
        self.job("/api/backups/restore", {"backupName": recovered_full})
        assert self.snapshot() == recovered_snapshot
        self.results["restoreAfterPITR"] = recovered_snapshot
        self.job("/api/backups/restore", {"backupName": delta2})
        assert self.snapshot() == baseline
        self.results["incrementalRestore"] = baseline
        # A valid past timestamp beyond all archived commits must fail, even
        # if pg_ctl briefly reports success while hot standby is available.
        self.job("/api/backups/trigger", {"kind": "full"})
        latest = self.data("/api/backups")["lastBackup"]
        unreachable = self.sql("SELECT to_char(clock_timestamp() AT TIME ZONE 'UTC', "
                               "'YYYY-MM-DD\"T\"HH24:MI:SS.US\"Z\"') AS target")[0]["target"]
        self.archive()
        failed = self.job("/api/backups/restore", {"backupName": latest, "targetTime": unreachable}, "failed")
        assert "recovery target" in failed.get("details", ""), failed
        self.results["unreachableTarget"] = failed
        # The API stays available and permits a retry after PostgreSQL exits.
        self.job("/api/backups/restore", {"backupName": delta2})
        assert self.snapshot() == baseline
        self.results["retryAfterFailure"] = "passed"

    def cleanup(self):
        self.docker("stop", "-t", "40", self.db, check=False)
        for name in [self.db, self.silo]:
            result = self.docker("logs", name, check=False)
            (self.output / ("postgres.log" if name == self.db else "silo.log")).write_text(result.stdout + result.stderr)
            self.docker("rm", "-fv", name, check=False)
        self.docker("network", "rm", self.prefix, check=False)
        self.docker("volume", "rm", self.volume, check=False)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--image", required=True, help="PGAIO runtime image with PostgreSQL and WAL-G")
    parser.add_argument("--output", type=Path, default=Path(tempfile.mkdtemp(prefix="pgaio-backup-results-")))
    args = parser.parse_args()
    args.output.mkdir(parents=True, exist_ok=True)
    suite = Suite(args.image, args.output.resolve())
    print("Test artifacts:", suite.output, flush=True)
    with tempfile.TemporaryDirectory(prefix="pgaio-backup-runtime-") as work:
        try:
            suite.start(Path(work))
            suite.test()
            suite.results["result"] = "passed"
        except Exception as error:
            suite.results["result"] = "failed"
            suite.results["error"] = str(error)
            raise
        finally:
            suite.cleanup()
            (suite.output / "results.json").write_text(json.dumps(suite.results, indent=2) + "\n")
    print("PASS: incremental backup/restore and PITR through the admin API", flush=True)


if __name__ == "__main__":
    main()
