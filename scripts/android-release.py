#!/usr/bin/env python3
"""Android release policy. stdout is JSON; failures are never a skip."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile

APK = "legato-android.apk"
CHECKSUM = "legato-android-SHA256SUMS"
INPUTS = ["mobile/", "themes/palettes.json", "scripts/mobile-android.sh",
          "scripts/android-release.py", ".github/workflows/release.yml"]


def run(*args):
    return subprocess.check_output(args, text=True).strip()


def releases():
    page = 1
    while True:
        batch = json.loads(run("gh", "api", f"repos/{{owner}}/{{repo}}/releases?per_page=100&page={page}"))
        if not isinstance(batch, list):
            raise ValueError("GitHub releases response must be an array")
        if not batch:
            return
        yield from batch
        page += 1


def version(tag):
    if not re.fullmatch(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)", tag):
        raise ValueError(f"not a stable Android release tag: {tag}")
    major, minor, patch = map(int, tag[1:].split("."))
    if major > 2098 or minor > 999 or patch > 999:
        raise ValueError("Android version range exceeded (major <=2098, minor/patch <=999)")
    return dict(versionName=tag[1:], versionCode=(major + 1) * 1000000 + minor * 1000 + patch)


def android_releases():
    return [r for r in releases() if not r["draft"] and not r["prerelease"] and any(
        a["name"] == APK and a["state"] == "uploaded" and a["size"] > 0 for a in r["assets"])]


def preflight(tag):
    current = version(tag)
    for release in android_releases():
        if version(release["tag_name"])["versionCode"] > current["versionCode"]:
            raise ValueError(f"newer Android release already published: {release['tag_name']}")
    return current


def decide(mode, source):
    if mode != "auto":
        return dict(build=mode == "always", baseline=None, reason=f"android={mode}")
    eligible = android_releases()
    if not eligible:
        return dict(build=True, baseline=None, reason="first Android publication")
    baseline = max(eligible, key=lambda r: (r["published_at"], r["tag_name"]))["tag_name"]
    version(baseline)
    ref = f"refs/tags/{baseline}^{{commit}}"
    resolved = subprocess.run(["git", "rev-parse", "--verify", ref], text=True, capture_output=True)
    if resolved.returncode:
        run("git", "fetch", "--no-tags", "origin", f"refs/tags/{baseline}")
        ref = "FETCH_HEAD^{commit}"
    commit = run("git", "rev-parse", "--verify", ref)
    result = subprocess.run(["git", "diff", "--quiet", commit, source, "--", *INPUTS])
    if result.returncode not in (0, 1):
        raise ValueError("cannot compare Android input trees")
    changed = result.returncode == 1
    return dict(build=changed, baseline=baseline, reason="Android inputs changed" if changed else "Android inputs unchanged")


def verify(tag, apk):
    expected = version(tag)
    digest = os.environ.get("LEGATO_ANDROID_CERT_SHA256", "").lower().replace(":", "")
    if not re.fullmatch(r"[0-9a-f]{64}", digest):
        raise ValueError("LEGATO_ANDROID_CERT_SHA256 must pin the approved signing certificate")
    if digest == "fac61745dc0903786fb9ede62a962b399f7348f0bb6f899b8332667591033b9c":
        raise ValueError("the public debug certificate cannot sign official APKs")
    tools = Path(os.environ["ANDROID_HOME"]) / "build-tools/36.0.0"
    certs = run(str(tools / "apksigner"), "verify", "--verbose", "--print-certs", str(apk))
    signers = re.findall(r"Signer #\d+ certificate SHA-256 digest: ([0-9a-fA-F]+)", certs)
    if [s.lower() for s in signers] != [digest]:
        raise ValueError("APK signer does not match the single approved certificate")
    metadata = run(str(tools / "aapt"), "dump", "badging", str(apk))
    package = re.search(r"^package: name='([^']+)' versionCode='([0-9]+)' versionName='([^']+)'", metadata, re.M)
    if not package or package.groups() != ("dev.legato.mobile", str(expected["versionCode"]), expected["versionName"]):
        raise ValueError("packaged application ID/version does not match release tag")
    return dict(**expected, applicationId="dev.legato.mobile", certificateSHA256=digest)


def asset_names(tag):
    release = json.loads(run("gh", "release", "view", tag, "--json", "assets"))
    return {asset["name"] for asset in release["assets"]}


def download(tag, name, directory):
    run("gh", "release", "download", tag, "--pattern", name, "--dir", str(directory))


def stage(tag, directory):
    version(tag)
    directory.mkdir(parents=True, exist_ok=True)
    if APK not in asset_names(tag):
        return dict(build=True, reason="no published APK at this tag")
    download(tag, APK, directory)
    metadata = verify(tag, directory / APK)
    return dict(build=False, reason="reuse immutable published APK", **metadata)


def publish(tag, directory):
    verify(tag, directory / APK)
    checksum = f"{hashlib.sha256((directory / APK).read_bytes()).hexdigest()}  {APK}\n"
    names = asset_names(tag)
    with tempfile.TemporaryDirectory() as scratch:
        remote = Path(scratch)
        if APK in names:
            download(tag, APK, remote)
            if (remote / APK).read_bytes() != (directory / APK).read_bytes():
                raise ValueError("refusing to replace an immutable published APK; stage the original first")
        else:
            run("gh", "release", "upload", tag, str(directory / APK))
        if CHECKSUM in names:
            download(tag, CHECKSUM, remote)
            if (remote / CHECKSUM).read_text() != checksum:
                raise ValueError("published Android checksum does not match APK; manual investigation required")
        else:
            (directory / CHECKSUM).write_text(checksum)
            run("gh", "release", "upload", tag, str(directory / CHECKSUM))
    return dict(published=True, apk=APK, checksum=CHECKSUM)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    decision = commands.add_parser("decide")
    decision.add_argument("mode", choices=["auto", "always", "never"])
    decision.add_argument("source", nargs="?", default="HEAD")
    for name in ["version", "preflight"]:
        commands.add_parser(name).add_argument("tag")
    verification = commands.add_parser("verify")
    verification.add_argument("tag")
    verification.add_argument("apk", type=Path)
    for name in ["stage", "publish"]:
        command = commands.add_parser(name)
        command.add_argument("tag")
        command.add_argument("directory", type=Path)
    args = parser.parse_args()
    if args.command == "decide":
        result = decide(args.mode, args.source)
    elif args.command == "version":
        result = version(args.tag)
    elif args.command == "verify":
        result = verify(args.tag, args.apk)
    elif args.command == "stage":
        result = stage(args.tag, args.directory)
    elif args.command == "publish":
        result = publish(args.tag, args.directory)
    else:
        result = preflight(args.tag)
    print(json.dumps(result))


if __name__ == "__main__":
    try:
        main()
    except (subprocess.CalledProcessError, ValueError, KeyError, OSError) as error:
        print(f"Android release failed: {error}", file=sys.stderr)
        sys.exit(1)
