"""A failed browser startup must release the temporary profile and frames."""
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


class CaptureTest(unittest.TestCase):
    def test_browser_exit_cleans_temporary_directories(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            scratch = root / "scratch"
            scratch.mkdir()
            result = subprocess.run(
                [sys.executable, "-I", str(Path(__file__).with_name("capture.py")),
                 "--input", temp, "--output", temp, "--artifacts", str(root / "artifacts"),
                 "--chrome", "/usr/bin/false"],
                capture_output=True, timeout=10, env={**os.environ, "TMPDIR": str(scratch)},
            )
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(list(scratch.iterdir()), [])


if __name__ == "__main__":
    unittest.main()
