#!/usr/bin/env python3
"""Exercise smoke process evidence against real, isolated Linux processes."""
import shutil
from pathlib import Path
import subprocess
import tempfile
import unittest

from smoke_process import plugin_process_identity


@unittest.skipUnless(Path("/proc/self/stat").is_file(), "Linux /proc required")
class ProcessIdentityTest(unittest.TestCase):
    def test_no_single_and_duplicate_processes(self):
        with tempfile.TemporaryDirectory(prefix="slack-process-test-") as tmp:
            executable = Path(tmp) / "sleep"
            shutil.copy2(shutil.which("sleep"), executable)
            processes = []
            try:
                self.assertIsNone(plugin_process_identity(executable))
                processes.append(subprocess.Popen([str(executable), "30"]))
                identity = plugin_process_identity(executable)
                self.assertEqual(identity[0], processes[0].pid)
                self.assertTrue(identity[1])
                processes.append(subprocess.Popen([str(executable), "30"]))
                with self.assertRaisesRegex(RuntimeError, "multiple plugin processes"):
                    plugin_process_identity(executable)
                for process in processes:
                    process.terminate()
                    process.wait(timeout=5)
                self.assertIsNone(plugin_process_identity(executable))
            finally:
                for process in processes:
                    if process.poll() is None:
                        process.terminate()
                        process.wait(timeout=5)


if __name__ == "__main__":
    unittest.main()
