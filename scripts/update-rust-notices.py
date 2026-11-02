#!/usr/bin/env python3
"""Regenerate locked dependency attribution from Cargo's verified source cache."""
import hashlib
import json
from pathlib import Path
import subprocess
import tomllib

ROOT = Path(__file__).resolve().parents[1]


def main():
    metadata = json.loads(subprocess.check_output(
        ["cargo", "metadata", "--locked", "--offline", "--format-version", "1"], cwd=ROOT
    ))
    lock = tomllib.loads((ROOT / "Cargo.lock").read_text())
    checksums = {(p["name"], p["version"]): p.get("checksum") for p in lock["package"]}
    supplements = json.loads((ROOT / "LICENSES/rust-supplements.json").read_text())
    root_package = next(p for p in metadata["packages"] if p["name"] == "t3oxide")
    direct = {p["name"] for p in root_package["dependencies"]}
    inventory = []
    notices = [
        "t3oxide Rust dependency notices\n",
        "This inventory covers the complete Cargo.lock graph, including build and\n"
        "platform-specific dependencies. A particular binary links only its enabled\n"
        "target graph. Upstream files are used unmodified. Source archives are\n"
        "available from the exact URLs below, with Cargo.lock SHA-256 checksums.\n\n"
        "nucleo-matcher is covered by MPL-2.0. Its complete corresponding source\n"
        "is available at https://crates.io/api/v1/crates/nucleo-matcher/0.3.1/download.\n"
        "No MPL-covered source file is modified or copied into t3oxide source.\n"
    ]
    for package in sorted(metadata["packages"], key=lambda p: (p["name"], p["version"])):
        if not package.get("source"):
            continue
        name, version = package["name"], package["version"]
        source = Path(package["manifest_path"]).parent
        files = sorted(p for p in source.iterdir() if p.is_file() and
                       p.name.upper().startswith(("LICENSE", "COPYING", "NOTICE", "COPYRIGHT")))
        texts = [(p.name, p.read_text(errors="strict"), None) for p in files]
        if not texts:
            supplement = supplements.get(name)
            if name.startswith("winapi-"):
                supplement = supplements["winapi"]
            if supplement is None:
                raise SystemExit(f"Missing reviewed license text: {name} {version}")
            texts = [("upstream license", supplement["text"], supplement["url"])]
        archive = f"https://crates.io/api/v1/crates/{name}/{version}/download"
        vcs_file = source / ".cargo_vcs_info.json"
        vcs = json.loads(vcs_file.read_text()) if vcs_file.exists() else {}
        item = {
            "name": name, "version": version, "direct": name in direct,
            "license": package["license"], "source": archive,
            "checksum": checksums[(name, version)], "repository": package["repository"],
            "commit": vcs.get("git", {}).get("sha1"),
            "licenseFiles": [{"path": path, "sha256": hashlib.sha256(text.encode()).hexdigest(),
                              **({"source": url} if url else {})} for path, text, url in texts],
        }
        inventory.append(item)
        notices.append(f"\n{'=' * 78}\n{name} {version}\nLicense: {package['license']}\nSource: {archive}\n")
        for path, text, url in texts:
            notices.append(f"\n--- {path}{' (' + url + ')' if url else ''} ---\n{text.rstrip()}\n")
    (ROOT / "LICENSES/rust-third-party.txt").write_text("".join(notices))
    (ROOT / "docs/dependencies.json").write_text(json.dumps({
        "formatVersion": 1, "application": "t3oxide-rust", "lockfile": "Cargo.lock",
        "scope": "complete locked graph, including platform-specific and build dependencies",
        "dependencies": inventory,
    }, indent=2) + "\n")
    print(f"Updated notices for {len(inventory)} locked upstream packages.")


if __name__ == "__main__":
    main()
