#!/usr/bin/env python3
"""Resolve or build the pinned mergerfs RPM; publish only from the trusted CI job."""

import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
from urllib.error import HTTPError
from urllib.parse import quote
from urllib.request import Request, urlopen

RECIPE = Path(__file__).resolve().parent
ROOT = RECIPE.parent.parent
OUTPUT = ROOT / "build_files/mergerfs"


def run(*args):
    return subprocess.check_output(args, text=True).strip()


def release(repository, tag):
    headers = {"Accept": "application/vnd.github+json"}
    if token := os.environ.get("GH_TOKEN"):
        headers["Authorization"] = f"Bearer {token}"
    request = Request(
        f"https://api.github.com/repos/{repository}/releases/tags/{quote(tag, safe='')}",
        headers=headers,
    )
    try:
        with urlopen(request, timeout=60) as response:
            data = json.load(response)
        if not isinstance(data["assets"], list):
            raise ValueError("Invalid release assets")
        return data
    except HTTPError as error:
        if error.code == 404:
            return None
        raise


def asset_named(data, pattern):
    matches = [a for a in data["assets"] if re.fullmatch(pattern, a["name"])]
    if len(matches) > 1:
        raise ValueError(f"Ambiguous release assets: {pattern}")
    return matches[0] if matches else None


def sha256(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def download(asset, destination):
    digest = asset.get("digest", "")
    if not isinstance(digest, str) or not re.fullmatch(r"sha256:[a-f0-9]{64}", digest):
        raise ValueError(f"Missing SHA256 digest for {asset['name']}")
    with urlopen(asset["browser_download_url"], timeout=60) as response:
        destination.write_bytes(response.read())
    if sha256(destination) != digest.removeprefix("sha256:"):
        destination.unlink()
        raise ValueError(f"Checksum mismatch for {asset['name']}")


def fallback_identity(config, fedora):
    builder = config["builders"].get(fedora)
    if not builder:
        raise ValueError(f"No upstream RPM or pinned Fedora {fedora} builder; update packages/mergerfs/config.json")
    recipe = hashlib.sha256()
    for name in ("Containerfile", "build.sh", "smoke-test.sh"):
        recipe.update(name.encode() + b"\0" + (RECIPE / name).read_bytes())
    inputs = {
        "version": config["version"], "source_sha256": config["source_sha256"],
        "fedora": fedora, "architecture": "x86_64", "builder": builder,
        "recipe_sha256": recipe.hexdigest(),
    }
    identity = hashlib.sha256(json.dumps(inputs, sort_keys=True).encode()).hexdigest()[:16]
    return f"mergerfs-{config['version']}-fc{fedora}-x86_64-{identity}", inputs


def build_rpm(config, tag, inputs):
    image = f"oldtimer-mergerfs:{tag}"
    subprocess.run([
        "docker", "build", "--tag", image, "--file", str(RECIPE / "Containerfile"),
        "--build-arg", f"BUILDER_IMAGE={inputs['builder']}",
        "--build-arg", f"VERSION={config['version']}",
        "--build-arg", f"SOURCE_SHA256={config['source_sha256']}", str(RECIPE),
    ], check=True)
    subprocess.run([
        "docker", "run", "--rm", "--device=/dev/fuse", "--cap-add=SYS_ADMIN",
        "--security-opt=apparmor=unconfined", image, "bash", "/recipe/smoke-test.sh",
    ], check=True)
    container = run("docker", "create", image)
    try:
        subprocess.run(["docker", "cp", f"{container}:/output/package.rpm", str(OUTPUT / "package.rpm")], check=True)
    finally:
        subprocess.run(["docker", "rm", container], check=True)


def resolve(config, fedora, repository):
    upstream = release("trapexit/mergerfs", config["version"])
    if upstream is None or upstream.get("draft"):
        raise ValueError("Pinned upstream mergerfs release is unavailable")
    pattern = rf"mergerfs-{re.escape(config['version'])}-\d+\.fc{fedora}\.x86_64\.rpm"
    asset = asset_named(upstream, pattern)
    if asset:
        download(asset, OUTPUT / "package.rpm")
        return {"origin": "upstream", "version": config["version"], "asset": asset["browser_download_url"]}, ""

    tag, inputs = fallback_identity(config, fedora)
    existing = release(repository, tag)
    if existing is not None and not existing.get("draft"):
        asset = asset_named(existing, r"package\.rpm")
        if asset is None:
            raise ValueError(f"Published release {tag} has no package.rpm; refusing to replace it")
        download(asset, OUTPUT / "package.rpm")
        return {**inputs, "origin": "reused", "release_tag": tag}, ""

    build_rpm(config, tag, inputs)
    return {**inputs, "origin": "built", "release_tag": tag}, tag


def prepare():
    config = json.loads((RECIPE / "config.json").read_text())
    OUTPUT.mkdir(parents=True, exist_ok=True)
    # Invalidate the previous preparation before making any network requests.
    (OUTPUT / "base-image.txt").unlink(missing_ok=True)
    (OUTPUT / "SHA256SUMS").unlink(missing_ok=True)
    base = json.loads(run("skopeo", "inspect", "docker://ghcr.io/ublue-os/bazzite:stable"))
    fedora = base["Labels"]["org.opencontainers.image.version"].split(".")[0]
    if base["Architecture"] != "amd64" or not re.fullmatch(r"\d+", fedora):
        raise ValueError("Expected an x86_64 Bazzite image with a Fedora version label")
    repository = os.environ.get("GITHUB_REPOSITORY", config["release_repository"])
    provenance, publish_tag = resolve(config, fedora, repository)
    digest = sha256(OUTPUT / "package.rpm")
    (OUTPUT / "SHA256SUMS").write_text(f"{digest}  package.rpm\n")
    (OUTPUT / "provenance.json").write_text(json.dumps({**provenance, "rpm_sha256": digest}, indent=2) + "\n")
    base_image = f"ghcr.io/ublue-os/bazzite@{base['Digest']}"
    (OUTPUT / "base-image.txt").write_text(base_image + "\n")
    if output := os.environ.get("GITHUB_OUTPUT"):
        with open(output, "a") as stream:
            stream.write(f"base_image={base_image}\npublish_tag={publish_tag}\n")
    print(f"mergerfs {config['version']} for Fedora {fedora}: {provenance['origin']}")


def publish():
    config = json.loads((RECIPE / "config.json").read_text())
    repository = os.environ["GITHUB_REPOSITORY"]
    if os.environ["GITHUB_REF"] != "refs/heads/main" or os.environ["GITHUB_EVENT_NAME"] not in ("push", "schedule", "workflow_dispatch"):
        raise ValueError("RPM publication requires a trusted main branch run")
    provenance = json.loads((OUTPUT / "provenance.json").read_text())
    tag, inputs = fallback_identity(config, provenance["fedora"])
    if provenance != {**inputs, "origin": "built", "release_tag": tag, "rpm_sha256": sha256(OUTPUT / "package.rpm")}:
        raise ValueError("RPM provenance does not match the checked-out build recipe")
    # The tag endpoint cannot find pending drafts; the release list includes them.
    pages = json.loads(run("gh", "api", f"repos/{repository}/releases?per_page=100", "--paginate", "--slurp"))
    existing = next((item for page in pages for item in page if item["tag_name"] == tag), None)
    if existing is not None and not existing.get("draft"):
        raise ValueError(f"Refusing to overwrite published release {tag}")
    if existing is None:
        run("gh", "release", "create", tag, "--repo", repository, "--draft", "--latest=false",
            "--target", os.environ["GITHUB_SHA"], "--title", tag, "--notes-file", str(OUTPUT / "provenance.json"))
    # A cancelled run can leave a draft; only unpublished assets may be replaced.
    run("gh", "release", "upload", tag, "--repo", repository, "--clobber",
        *(str(OUTPUT / name) for name in ("package.rpm", "SHA256SUMS", "provenance.json")))
    run("gh", "release", "edit", tag, "--repo", repository, "--draft=false", "--latest=false")


if __name__ == "__main__":
    try:
        if sys.argv[1:] == ["publish"]:
            publish()
        elif not sys.argv[1:]:
            prepare()
        else:
            raise ValueError("Usage: prepare.py [publish]")
    except (ValueError, KeyError, OSError, subprocess.CalledProcessError) as error:
        sys.exit(f"ERROR: mergerfs preparation failed: {error}")
