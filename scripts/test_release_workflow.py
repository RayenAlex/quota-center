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


if __name__ == "__main__":
    unittest.main()
