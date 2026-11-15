"""Exercise the release command at its subprocess/git/GitHub boundary."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

SCRIPT = Path(__file__).with_name("android-release.py")


class AndroidReleaseTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.env = dict(os.environ, PATH=f"{self.root}:{os.environ['PATH']}")
        self.git("init", "-q")
        self.git("config", "user.email", "test@example.invalid")
        self.git("config", "user.name", "Test")
        self.change("mobile/app.txt")
        self.pages = self.root / "pages.json"
        self.env["TEST_PAGES"] = str(self.pages)
        self.pages.write_text(json.dumps([[]]))
        gh = self.root / "gh"
        gh.write_text('''#!/usr/bin/env python3
import json, os, sys
from pathlib import Path
pages = json.loads(Path(os.environ["TEST_PAGES"]).read_text())
if pages == "error":
    sys.exit(1)
if sys.argv[1] == "api":
    page = int(sys.argv[-1].split("page=")[-1])
    print(json.dumps(pages[page - 1] if page <= len(pages) else []))
else:
    import shutil
    assets = Path(os.environ["TEST_ASSETS"])
    action = sys.argv[2]
    if action == "view":
        print(json.dumps({"assets": [{"name": p.name} for p in assets.iterdir()]}))
    elif action == "download":
        pattern = sys.argv[sys.argv.index("--pattern") + 1]
        directory = Path(sys.argv[sys.argv.index("--dir") + 1])
        shutil.copyfile(assets / pattern, directory / pattern)
    elif action == "upload":
        file = Path(sys.argv[4])
        if file.name == os.environ.get("TEST_FAIL_UPLOAD") or (assets / file.name).exists():
            sys.exit(1)
        shutil.copyfile(file, assets / file.name)
    else:
        sys.exit(2)
''')
        gh.chmod(0o755)
        self.assets = self.root / "assets"
        self.assets.mkdir()
        self.env["TEST_ASSETS"] = str(self.assets)

    def git(self, *args):
        return subprocess.check_output(["git", *args], cwd=self.root, stderr=subprocess.DEVNULL, text=True).strip()

    def change(self, path):
        file = self.root / path
        file.parent.mkdir(parents=True, exist_ok=True)
        file.write_text(file.read_text() + "change\n" if file.exists() else "change\n")
        self.git("add", path)
        self.git("commit", "-qm", "change")

    def command(self, *args, ok=True):
        result = subprocess.run([sys.executable, str(SCRIPT), *args], cwd=self.root, env=self.env, text=True, capture_output=True)
        if ok:
            self.assertEqual(result.returncode, 0, result.stderr)
            return json.loads(result.stdout)
        self.assertNotEqual(result.returncode, 0, result.stdout)
        return result

    def releases(self, *pages):
        self.pages.write_text(json.dumps(pages))

    def release(self, tag, apk=True, **extra):
        return dict(tag_name=tag, draft=False, prerelease=False, published_at="2026-01-01T00:00:00Z", assets=[dict(name="legato-android.apk", size=123, state="uploaded")] if apk else [], **extra)

    def test_first_release_and_explicit_modes(self):
        self.assertTrue(self.command("decide", "auto")["build"])
        self.assertTrue(self.command("decide", "always")["build"])
        self.assertFalse(self.command("decide", "never")["build"])
        self.command("decide", "sometimes", ok=False)

    def test_deep_baseline_skips_server_changes_but_not_packaged_inputs(self):
        self.git("tag", "v0.1.0")
        self.releases([self.release("v0.3.0", apk=False)], [self.release("v0.2.0", apk=False)], [self.release("v0.1.0")])
        self.change("internal/server/server.go")
        decision = self.command("decide", "auto")
        self.assertFalse(decision["build"])
        self.assertEqual(decision["baseline"], "v0.1.0")
        for path in ["mobile/gradle/wrapper/gradle-wrapper.properties", "themes/palettes.json", "scripts/mobile-android.sh", "scripts/android-release.py", ".github/workflows/release.yml"]:
            with self.subTest(path=path):
                self.change(path)
                self.assertTrue(self.command("decide", "auto")["build"])
                self.git("tag", "-f", "v0.1.0")

    def test_api_and_missing_tag_fail_closed(self):
        self.pages.write_text(json.dumps("error"))
        self.command("decide", "auto", ok=False)
        self.pages.write_text(json.dumps([{}]))
        self.command("decide", "auto", ok=False)
        self.releases([self.release("v0.1.0")])
        self.command("decide", "auto", ok=False)

    def test_missing_local_tag_is_fetched_from_origin(self):
        remote = self.root / "remote.git"
        self.git("clone", "--bare", ".", str(remote))
        self.git("--git-dir", str(remote), "tag", "v0.1.0")
        self.git("remote", "add", "origin", str(remote))
        self.releases([self.release("v0.1.0")])
        self.change("internal/server/server.go")
        self.assertFalse(self.command("decide", "auto")["build"])

    def test_compares_exact_source_and_ignores_ineligible_assets(self):
        self.git("tag", "v0.1.0")
        self.change("mobile/app.txt")
        draft = self.release("v0.2.0")
        draft["draft"] = True
        failed = self.release("v0.3.0")
        failed["assets"][0]["state"] = "new"
        self.releases([draft, failed, self.release("v0.1.0")])
        self.assertFalse(self.command("decide", "auto", "v0.1.0")["build"])
        self.assertTrue(self.command("decide", "auto")["build"])

    def test_version_codes_are_deterministic_ordered_and_bounded(self):
        for tag, code in [("v0.0.1", 1000001), ("v0.9.999", 1009999), ("v0.10.0", 1010000), ("v1.0.0", 2000000), ("v2098.999.999", 2099999999)]:
            with self.subTest(tag=tag):
                for _ in range(2):
                    self.assertEqual(self.command("version", tag), dict(versionName=tag[1:], versionCode=code))
        for tag in ["v2099.0.0", "v1.1000.0", "v1.0.1000", "v1.0.0-rc.1", "v01.0.0", "1.0.0"]:
            self.command("version", tag, ok=False)

    def test_preflight_rejects_out_of_order_but_allows_same_tag_retry(self):
        self.releases([self.release("v1.0.0")])
        self.command("preflight", "v0.9.0", ok=False)
        self.command("preflight", "v1.0.0")
        self.command("preflight", "v1.0.1")

    def package_tools(self):
        self.env["LEGATO_ANDROID_CERT_SHA256"] = "ab" * 32
        self.env["ANDROID_HOME"] = str(self.root / "sdk")
        tools = self.root / "sdk/build-tools/36.0.0"
        tools.mkdir(parents=True)
        aapt = tools / "aapt"
        aapt.write_text("#!/bin/sh\necho \"package: name='dev.legato.mobile' versionCode='2000001' versionName='1.0.1'\"\n")
        signer = tools / "apksigner"
        signer.write_text("#!/bin/sh\necho 'Signer #1 certificate SHA-256 digest: " + "ab" * 32 + "'\n")
        for tool in [aapt, signer]:
            tool.chmod(0o755)

    def test_verification_inspects_packaged_identity_version_and_signer(self):
        self.package_tools()
        self.command("verify", "v1.0.1", "test.apk")
        self.command("verify", "v1.0.2", "test.apk", ok=False)
        self.env["LEGATO_ANDROID_CERT_SHA256"] = "cd" * 32
        self.command("verify", "v1.0.1", "test.apk", ok=False)
        self.env["LEGATO_ANDROID_CERT_SHA256"] = ""
        self.command("verify", "v1.0.1", "test.apk", ok=False)

    def test_partial_upload_recovers_original_apk_without_replacement(self):
        self.package_tools()
        dist = self.root / "dist"
        self.assertTrue(self.command("stage", "v1.0.1", str(dist))["build"])
        apk = dist / "legato-android.apk"
        apk.write_bytes(b"original signed APK")
        self.env["TEST_FAIL_UPLOAD"] = "legato-android-SHA256SUMS"
        self.command("publish", "v1.0.1", str(dist), ok=False)
        self.assertEqual((self.assets / apk.name).read_bytes(), apk.read_bytes())
        del self.env["TEST_FAIL_UPLOAD"]
        retry = self.root / "retry"
        self.assertFalse(self.command("stage", "v1.0.1", str(retry))["build"])
        self.command("publish", "v1.0.1", str(retry))
        checksum = hashlib.sha256(b"original signed APK").hexdigest()
        self.assertEqual((self.assets / "legato-android-SHA256SUMS").read_text(), f"{checksum}  {apk.name}\n")
        apk.write_bytes(b"different rebuilt APK")
        self.command("publish", "v1.0.1", str(dist), ok=False)
        self.assertEqual((self.assets / apk.name).read_bytes(), b"original signed APK")

    def test_apk_upload_failure_is_visible_and_does_not_publish(self):
        self.package_tools()
        dist = self.root / "dist"
        dist.mkdir()
        (dist / "legato-android.apk").write_bytes(b"APK")
        self.env["TEST_FAIL_UPLOAD"] = "legato-android.apk"
        self.command("publish", "v1.0.1", str(dist), ok=False)
        self.assertEqual(list(self.assets.iterdir()), [])


if __name__ == "__main__":
    unittest.main()
