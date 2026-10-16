"""Offline checks for release selection, archive integrity and formula generation."""

import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch


spec = importlib.util.spec_from_file_location(
    "update_homebrew", Path(__file__).with_name("update-homebrew.py")
)
homebrew = importlib.util.module_from_spec(spec)
spec.loader.exec_module(homebrew)


class HomebrewReleaseTest(unittest.TestCase):
    def setUp(self):
        self.release = {"tagName": "v0.1.0", "isDraft": False, "isPrerelease": False}
        self.latest = "v0.1.0"
        self.corrupt = False
        self.missing = False

    def gh(self, *args):
        if args[:2] == ("release", "view"):
            return json.dumps(self.release) if args[2] == "v0.1.0" else self.latest
        self.assertEqual(args[:2], ("release", "download"))
        directory = Path(args[args.index("--dir") + 1])
        lines = []
        for platform in homebrew.PLATFORMS:
            filename = f"cchp_0.1.0_{platform}.tar.gz"
            payload = platform.encode()
            checksum = hashlib.sha256(payload).hexdigest()
            if not self.missing or platform != "linux_arm64":
                (directory / filename).write_bytes(b"corrupt" if self.corrupt else payload)
            lines.append(f"{checksum}  {filename}")
        (directory / "checksums.txt").write_text("\n".join(lines))
        return ""

    def test_verified_release_supports_all_four_platforms_and_offline_smoke_test(self):
        with tempfile.TemporaryDirectory() as directory, patch.object(homebrew, "gh", self.gh):
            output = Path(directory) / "Formula/cchp.rb"
            homebrew.update("v0.1.0", output)
            formula = output.read_text()
            for platform in homebrew.PLATFORMS:
                self.assertIn(f"cchp_0.1.0_{platform}.tar.gz", formula)
                self.assertIn(hashlib.sha256(platform.encode()).hexdigest(), formula)
            self.assertIn('generate_completions_from_executable(bin/"cchp", "completion")', formula)
            self.assertIn("getUsageSummary", formula)

    def test_invalid_or_prerelease_tag_never_calls_github(self):
        with patch.object(homebrew, "gh") as gh:
            for tag in ("v0.2.0-rc.1", "v01.2.3", "0.1.0", "--help", 'v0.1.0"'):
                with self.subTest(tag=tag), self.assertRaises(ValueError):
                    homebrew.update(tag, Path("unused"))
            gh.assert_not_called()

    def test_draft_prerelease_and_older_release_are_rejected(self):
        with patch.object(homebrew, "gh", self.gh):
            for release, latest in (
                ({**self.release, "isDraft": True}, "v0.1.0"),
                ({**self.release, "isPrerelease": True}, "v0.1.0"),
                (self.release, "v0.2.0"),
            ):
                self.release, self.latest = release, latest
                with self.subTest(release=release, latest=latest), self.assertRaises(ValueError):
                    homebrew.update("v0.1.0", Path("unused"))

    def test_bad_or_missing_archive_preserves_existing_formula(self):
        with tempfile.TemporaryDirectory() as directory, patch.object(homebrew, "gh", self.gh):
            output = Path(directory) / "cchp.rb"
            output.write_text("existing formula")
            for corrupt, missing, error in ((True, False, ValueError), (False, True, FileNotFoundError)):
                self.corrupt, self.missing = corrupt, missing
                with self.subTest(corrupt=corrupt, missing=missing), self.assertRaises(error):
                    homebrew.update("v0.1.0", output)
                self.assertEqual(output.read_text(), "existing formula")


if __name__ == "__main__":
    unittest.main()
