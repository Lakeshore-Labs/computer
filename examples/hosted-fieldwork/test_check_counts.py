from contextlib import redirect_stdout
import io
from pathlib import Path
import runpy
import unittest
from unittest.mock import patch


class CountsTest(unittest.TestCase):
    def test_missing_receipts_fail(self):
        with patch.object(Path, "glob", return_value=[]), redirect_stdout(io.StringIO()), \
                self.assertRaises(SystemExit) as exit:
            runpy.run_path(str(Path(__file__).with_name("check_counts.py")))
        self.assertNotEqual(exit.exception.code, 0)


if __name__ == "__main__":
    unittest.main()
