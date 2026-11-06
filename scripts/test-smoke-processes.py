#!/usr/bin/env python3
"""Exercise smoke process evidence against real, isolated Linux processes."""
import shutil
from pathlib import Path
import subprocess
import tempfile
import time
import unittest

from smoke_process import plugin_process_identity


@unittest.skipUnless(Path("/proc/self/stat").is_file(), "Linux /proc required")
class ProcessIdentityTest(unittest.TestCase):
    def wait_for_exec(self, process, executable):
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            self.assertIsNone(process.poll(), "probe exited before exec evidence")
            try:
                if (Path("/proc") / str(process.pid) / "exe").readlink() == executable:
                    return
            except OSError:
                pass
            time.sleep(0.01)
        self.fail("timed out waiting for probe executable")

    def test_no_single_and_duplicate_processes(self):
        with tempfile.TemporaryDirectory(prefix="slack-process-test-") as tmp:
            executable = Path(tmp).resolve() / "sleep"
            shutil.copy2(shutil.which("sleep"), executable)
            processes = []
            try:
                self.assertIsNone(plugin_process_identity(executable))
                processes.append(subprocess.Popen([str(executable), "30"]))
                self.wait_for_exec(processes[0], executable)
                identity = plugin_process_identity(executable)
                self.assertEqual(identity[0], processes[0].pid)
                self.assertTrue(identity[1])
                processes.append(subprocess.Popen([str(executable), "30"]))
                self.wait_for_exec(processes[1], executable)
                with self.assertRaisesRegex(RuntimeError, "multiple plugin processes"):
                    plugin_process_identity(executable)
                processes[1].terminate()
                processes[1].wait(timeout=5)
                executable.unlink()
                self.assertEqual(plugin_process_identity(executable), identity)
                shutil.copy2(shutil.which("sleep"), executable)
                processes.append(subprocess.Popen([str(executable), "30"]))
                self.wait_for_exec(processes[2], executable)
                with self.assertRaisesRegex(RuntimeError, "multiple plugin processes"):
                    plugin_process_identity(executable)
                for process in processes:
                    if process.poll() is None:
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
