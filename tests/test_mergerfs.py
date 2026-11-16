import importlib.util
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
from urllib.error import HTTPError

SPEC = importlib.util.spec_from_file_location("mergerfs", Path(__file__).resolve().parents[1] / "packages/mergerfs/prepare.py")
mergerfs = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(mergerfs)


class ResolverTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.output = Path(self.directory.name)
        self.patch_output = patch.object(mergerfs, "OUTPUT", self.output)
        self.patch_output.start()
        self.addCleanup(self.patch_output.stop)
        self.config = json.loads((mergerfs.RECIPE / "config.json").read_text())

    def test_upstream_rpm_avoids_local_release_lookup_and_build(self):
        asset = {"name": "mergerfs-2.42.0-1.fc45.x86_64.rpm", "browser_download_url": "https://example.com/rpm"}
        with patch.object(mergerfs, "release", return_value={"assets": [asset]}) as release, \
                patch.object(mergerfs, "download") as download, patch.object(mergerfs, "build_rpm") as build:
            provenance, tag = mergerfs.resolve(self.config, "45", "owner/repo")
        self.assertEqual(provenance["origin"], "upstream")
        self.assertEqual(tag, "")
        release.assert_called_once_with("trapexit/mergerfs", "2.42.0")
        download.assert_called_once_with(asset, self.output / "package.rpm")
        build.assert_not_called()

    def test_reuse_and_missing_release(self):
        for existing, origin in [(None, "built"), ({"draft": True, "assets": []}, "built"),
                                 ({"assets": [{"name": "package.rpm"}]}, "reused")]:
            with self.subTest(origin=origin, existing=existing), \
                    patch.object(mergerfs, "release", side_effect=[{"assets": []}, existing]), \
                    patch.object(mergerfs, "download") as download, patch.object(mergerfs, "build_rpm") as build:
                provenance, publish_tag = mergerfs.resolve(self.config, "45", "owner/repo")
                self.assertEqual(provenance["origin"], origin)
                self.assertEqual(build.call_count, int(origin == "built"))
                self.assertEqual(download.call_count, int(origin == "reused"))
                self.assertEqual(bool(publish_tag), origin == "built")

    def test_incomplete_published_release_is_not_rebuilt(self):
        with patch.object(mergerfs, "release", side_effect=[{"assets": []}, {"assets": []}]), \
                patch.object(mergerfs, "build_rpm") as build:
            with self.assertRaisesRegex(ValueError, "refusing to replace"):
                mergerfs.resolve(self.config, "45", "owner/repo")
        build.assert_not_called()

    def test_http_errors_are_not_treated_as_missing_assets(self):
        for code in (403, 429, 500):
            error = HTTPError("url", code, "error", {}, io.BytesIO())
            self.addCleanup(error.close)
            with self.subTest(code=code), patch.object(mergerfs, "urlopen", side_effect=error):
                with self.assertRaises(HTTPError):
                    mergerfs.release("owner/repo", "tag")
        error = HTTPError("url", 404, "missing", {}, io.BytesIO())
        self.addCleanup(error.close)
        with patch.object(mergerfs, "urlopen", side_effect=error):
            self.assertIsNone(mergerfs.release("owner/repo", "tag"))

    def test_lookup_failure_does_not_build(self):
        with patch.object(mergerfs, "release", side_effect=OSError("network failure")), \
                patch.object(mergerfs, "build_rpm") as build:
            with self.assertRaises(OSError):
                mergerfs.resolve(self.config, "45", "owner/repo")
        build.assert_not_called()

    def test_malformed_api_response_fails(self):
        for content in (b"not json", b'{"assets":null}', b'{"message":"error"}'):
            with self.subTest(content=content), patch.object(mergerfs, "urlopen", return_value=io.BytesIO(content)):
                with self.assertRaises((ValueError, KeyError)):
                    mergerfs.release("owner/repo", "tag")

    def test_download_requires_matching_digest(self):
        data = b"rpm contents"
        digest = mergerfs.hashlib.sha256(data).hexdigest()
        asset = {"name": "package.rpm", "digest": f"sha256:{digest}", "browser_download_url": "https://example.com/rpm"}
        destination = self.output / "package.rpm"
        with patch.object(mergerfs, "urlopen", return_value=io.BytesIO(data)):
            mergerfs.download(asset, destination)
        self.assertEqual(destination.read_bytes(), data)
        with patch.object(mergerfs, "urlopen", return_value=io.BytesIO(b"corrupted")):
            with self.assertRaisesRegex(ValueError, "Checksum mismatch"):
                mergerfs.download(asset, destination)
        self.assertFalse(destination.exists())
        with self.assertRaisesRegex(ValueError, "Missing SHA256"):
            mergerfs.download({**asset, "digest": ""}, destination)

    def test_identity_changes_with_inputs_but_is_stable_otherwise(self):
        original = mergerfs.fallback_identity(self.config, "45")[0]
        self.assertEqual(original, mergerfs.fallback_identity(self.config, "45")[0])
        for key, value in [("version", "2.43.0"), ("source_sha256", "b" * 64),
                           ("builders", {"45": "different-builder"})]:
            self.assertNotEqual(original, mergerfs.fallback_identity({**self.config, key: value}, "45")[0])
        with patch.object(mergerfs, "RECIPE", self.output):
            for name in ("Containerfile", "build.sh", "smoke-test.sh"):
                (self.output / name).write_text("changed recipe")
            self.assertNotEqual(original, mergerfs.fallback_identity(self.config, "45")[0])
        with self.assertRaisesRegex(ValueError, "pinned Fedora 46 builder"):
            mergerfs.fallback_identity(self.config, "46")

    def test_publication_rejects_pull_requests(self):
        with patch.dict(mergerfs.os.environ, {"GITHUB_REPOSITORY": "owner/repo", "GITHUB_REF": "refs/heads/main", "GITHUB_EVENT_NAME": "pull_request"}), \
                patch.object(mergerfs, "run") as run:
            with self.assertRaisesRegex(ValueError, "trusted main"):
                mergerfs.publish()
        run.assert_not_called()

    def test_publication_is_draft_until_uploads_finish(self):
        (self.output / "package.rpm").write_bytes(b"rpm")
        tag, inputs = mergerfs.fallback_identity(self.config, "45")
        provenance = {**inputs, "origin": "built", "release_tag": tag, "rpm_sha256": mergerfs.sha256(self.output / "package.rpm")}
        (self.output / "provenance.json").write_text(json.dumps(provenance))
        env = {"GITHUB_REPOSITORY": "owner/repo", "GITHUB_REF": "refs/heads/main", "GITHUB_EVENT_NAME": "push", "GITHUB_SHA": "commit"}
        with patch.dict(mergerfs.os.environ, env):
            with patch.object(mergerfs, "run", side_effect=["[[]]", "", "", ""]) as run:
                mergerfs.publish()
                self.assertEqual([call.args[2] for call in run.call_args_list[1:]], ["create", "upload", "edit"])
                self.assertIn("--draft", run.call_args_list[1].args)
                self.assertIn("--latest=false", run.call_args_list[-1].args)
            published = json.dumps([[{"tag_name": tag, "draft": False}]])
            with patch.object(mergerfs, "run", return_value=published) as run:
                with self.assertRaisesRegex(ValueError, "overwrite published"):
                    mergerfs.publish()
                run.assert_called_once()
            # A pending draft is absent from /releases/tags; find it on a later list page.
            drafts = json.dumps([[], [{"tag_name": tag, "draft": True}]])
            with patch.object(mergerfs, "release", return_value=None) as release, \
                    patch.object(mergerfs, "run", side_effect=[drafts, "", ""]) as run:
                mergerfs.publish()
                release.assert_not_called()
                run.assert_any_call("gh", "api", "repos/owner/repo/releases?per_page=100", "--paginate", "--slurp")
                self.assertEqual([call.args[2] for call in run.call_args_list[1:]], ["upload", "edit"])
            with patch.object(mergerfs, "run", side_effect=[drafts, OSError("upload failed")]) as run:
                with self.assertRaises(OSError):
                    mergerfs.publish()
                self.assertEqual([call.args[2] for call in run.call_args_list[1:]], ["upload"])
            with patch.object(mergerfs, "run", side_effect=OSError("API failed")) as run:
                with self.assertRaises(OSError):
                    mergerfs.publish()
                run.assert_called_once()
            (self.output / "package.rpm").write_bytes(b"changed after testing")
            with patch.object(mergerfs, "run") as run:
                with self.assertRaisesRegex(ValueError, "provenance"):
                    mergerfs.publish()
                run.assert_not_called()


if __name__ == "__main__":
    unittest.main()
