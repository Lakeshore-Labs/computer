import asyncio
from pathlib import Path
import runpy
import sys
import tempfile
import unittest
from unittest.mock import Mock, patch
from zipfile import ZipFile


class ReplayTest(unittest.TestCase):
    def test_downloads_are_not_decompressed_before_the_client_connects(self):
        with patch.dict(sys.modules, mcp=Mock()):
            replay = runpy.run_path(str(Path(__file__).with_name("replay.py")))["replay"]
        connect = Mock(side_effect=ConnectionError("offline test"))
        with tempfile.TemporaryDirectory() as temp, patch.dict(replay.__globals__, connect=connect), \
                patch.object(ZipFile, "read", autospec=True, side_effect=ZipFile.read) as read:
            with self.assertRaises(ConnectionError):
                asyncio.run(replay(None, None, Path(temp) / "out"))
            self.assertFalse(any(name.startswith("assets/moon-study/") for (_, name), _ in read.call_args_list))


if __name__ == "__main__":
    unittest.main()
