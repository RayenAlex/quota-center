import unittest
from pathlib import Path


class ReleaseWorkflowTest(unittest.TestCase):
    def test_archive_paths_delimit_shell_variables(self):
        workflow = (
            Path(__file__).parents[1] / ".github" / "workflows" / "release.yml"
        ).read_text(encoding="utf-8")

        self.assertIn(
            '--archive "dist/quota-center_${VERSION}_${GOOS}_${GOARCH}.zip"',
            workflow,
        )
        self.assertIn(
            '--sha256 "dist/quota-center_${VERSION}_${GOOS}_${GOARCH}.zip.sha256"',
            workflow,
        )

    def test_release_assembles_aggregate_checksums_file(self):
        workflow = (
            Path(__file__).parents[1] / ".github" / "workflows" / "release.yml"
        ).read_text(encoding="utf-8")

        self.assertIn("Assemble aggregate checksums", workflow)
        self.assertIn(
            'cat "${checksum_files[@]}" | LC_ALL=C sort > release-artifacts/checksums.txt',
            workflow,
        )
        self.assertIn(
            'test "${#archive_files[@]}" -eq "${#checksum_files[@]}"',
            workflow,
        )
        self.assertIn("test -s release-artifacts/checksums.txt", workflow)


if __name__ == "__main__":
    unittest.main()
