"""Exercise the replay verdict and cleanup with offline stand-ins for external programs."""
import io
import json
from contextlib import redirect_stderr, redirect_stdout
from pathlib import Path
import runpy
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

SCRIPT = Path(__file__).with_name("run.py")


class ReplayTest(unittest.TestCase):
    def replay(self, accepted=None, stop_exit=0, structured=False):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        root = Path(temporary.name)
        cluster = root / "run"
        cluster.mkdir()
        output = root / "out"

        def program(cmd, **kwargs):
            status, stdout = 0, ""
            if any(str(arg).endswith("/prepare.py") for arg in cmd):
                (cluster / "pg.env").write_text("PGHOST=/test\nPGPORT=1234\n")
                (cluster / "pgdata").mkdir()
                (cluster / "loaded.parquet").touch()
            elif cmd[0] == "computer":
                for db, query, rejection in (
                    ("postgres", "DELETE FROM orders", "cannot execute DELETE in a read-only transaction"),
                    ("duckdb", "CREATE TABLE x", 'Cannot execute statement of type "CREATE" on database "loads" which is attached in read-only mode!'),
                ):
                    if query in cmd[-1] and accepted != db:
                        stdout += rejection + "\n"
                        status = 0 if "|| true" in cmd[-1] else 1
                        if structured and db == "duckdb":
                            status, stdout = 0, json.dumps({"success": False, "error": rejection})
                if "/scripts/guest.py" in cmd[-1]:
                    for filename in ("balance.json", "transfers.csv"):
                        (cluster / "work" / filename).touch()
            elif str(cmd[0]).endswith("/pg_ctl"):
                if isinstance(stop_exit, OSError):
                    raise stop_exit
                status = stop_exit
            return subprocess.CompletedProcess(cmd, status, stdout, "")

        args = [str(SCRIPT), "--support", str(root), "--pywasi", str(root), "--out", str(output)]
        with patch.object(sys, "argv", args), patch("tempfile.mkdtemp", return_value=str(cluster)), \
                patch("subprocess.run", side_effect=program), redirect_stdout(io.StringIO()), redirect_stderr(io.StringIO()), \
                self.assertRaises(SystemExit) as exit:
            runpy.run_path(str(SCRIPT), run_name="__main__")
        return exit.exception.code, json.loads((output / "receipt.json").read_text()), cluster

    def test_accepted_write_fails_the_replay(self):
        for db in ("postgres", "duckdb"):
            with self.subTest(db=db):
                code, receipt, _ = self.replay(accepted=db)
                self.assertNotEqual(code, 0)
                self.assertFalse(receipt["pass"])

    def test_duckdb_can_report_rejection_in_a_successful_tool_response(self):
        code, receipt, _ = self.replay(structured=True)
        self.assertEqual(code, 0)
        self.assertTrue(receipt["pass"])

    def test_unconfirmed_shutdown_preserves_cluster(self):
        for failure in (1, OSError("pg_ctl unavailable")):
            with self.subTest(failure=failure):
                code, receipt, cluster = self.replay(stop_exit=failure)
                self.assertTrue((cluster / "pgdata").is_dir())
                self.assertNotEqual(code, 0)
                self.assertFalse(receipt["pass"])

    def test_rejections_and_confirmed_shutdown_pass(self):
        code, receipt, cluster = self.replay()
        self.assertEqual(code, 0)
        self.assertTrue(receipt["pass"])
        self.assertFalse(cluster.exists())


if __name__ == "__main__":
    unittest.main()
