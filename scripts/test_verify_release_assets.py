import hashlib
import subprocess
import sys
import tempfile
import unittest
import zipfile
from pathlib import Path


class VerifyReleaseAssetsTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.archive = self.root / "quota-center_0.2.0_darwin_arm64.zip"
        self.checksum = self.root / "quota-center_0.2.0_darwin_arm64.zip.sha256"

    def tearDown(self):
        self.temp.cleanup()

    def write_archive(self, *names):
        with zipfile.ZipFile(self.archive, "w") as writer:
            for name in names:
                entry = zipfile.ZipInfo(name)
                entry.external_attr = 0o100755 << 16
                writer.writestr(entry, b"library")
        digest = hashlib.sha256(self.archive.read_bytes()).hexdigest()
        self.checksum.write_text(
            f"{digest}  {self.archive.name}\n",
            encoding="utf-8",
        )

    def run_verifier(self):
        return subprocess.run(
            [
                sys.executable,
                str(Path(__file__).with_name("verify_release_assets.py")),
                "--archive",
                str(self.archive),
                "--sha256",
                str(self.checksum),
            ],
            capture_output=True,
            text=True,
        )

    def test_accepts_single_root_plugin_library(self):
        self.write_archive("quota-center.dylib")

        result = self.run_verifier()

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("OK quota-center_0.2.0_darwin_arm64.zip", result.stdout)

    def test_rejects_additional_zip_entry(self):
        self.write_archive("quota-center.dylib", "README.txt")

        result = self.run_verifier()

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("exactly one ZIP entry", result.stderr)


if __name__ == "__main__":
    unittest.main()
