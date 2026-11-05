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
        "target graph. Vendored modifications are identified per package. Source archives are\n"
        "available from the exact URLs below, with Cargo.lock SHA-256 checksums\n"
        "for registry archives and exact commits for Git sources.\n\n"
        "License text below normalizes trailing whitespace; inventory hashes\n"
        "identify the original upstream files.\n\n"
        "nucleo-matcher is covered by MPL-2.0. Its complete corresponding source\n"
        "is available at https://crates.io/api/v1/crates/nucleo-matcher/0.3.1/download.\n"
        "No MPL-covered source file is modified or copied into t3oxide source.\n"
    ]
    for package in sorted(metadata["packages"], key=lambda p: (p["name"], p["version"])):
        if package["id"] == root_package["id"]:
            continue
        name, version = package["name"], package["version"]
        source = Path(package["manifest_path"]).parent
        vendor = (package.get("metadata") or {}).get("t3oxide-vendor")
        if not package.get("source") and not vendor:
            raise SystemExit(f"Unattributed local dependency: {name} {version}")
        files = sorted(p for p in source.iterdir() if p.is_file() and
                       p.name.upper().startswith(("LICENSE", "COPYING", "NOTICE", "COPYRIGHT")))
        if not files and (package["source"] or "").startswith("git+"):
            # Workspace crates may inherit the reviewed repository-root grant.
            # Stay inside the exact Cargo Git checkout, never the cache parent.
            git_root = Path(subprocess.check_output(
                ["git", "-C", str(source), "rev-parse", "--show-toplevel"], text=True
            ).strip())
            files = sorted(p for p in git_root.iterdir() if p.is_file() and
                           p.name.upper().startswith(("LICENSE", "COPYING", "NOTICE", "COPYRIGHT")))
        texts = [(str(p.relative_to(source)) if p.is_relative_to(source) else
                  str(Path("..") / p.name), p.read_text(errors="strict"), None) for p in files]
        if not texts:
            supplement = supplements.get(name)
            if name.startswith("winapi-"):
                supplement = supplements["winapi"]
            if supplement is None:
                raise SystemExit(f"Missing reviewed license text: {name} {version}")
            texts = [("upstream license", supplement["text"], supplement["url"])]
        archive = f"https://crates.io/api/v1/crates/{name}/{version}/download"
        git_commit = None
        if vendor:
            git_commit = vendor["upstream-commit"]
            archive = f"{package['repository']}/archive/{git_commit}.tar.gz"
        elif package["source"].startswith("git+"):
            git_commit = package["source"].rsplit("#", 1)[1]
            archive = f"{package['repository']}/archive/{git_commit}.tar.gz"
        vcs_file = source / ".cargo_vcs_info.json"
        vcs = json.loads(vcs_file.read_text()) if vcs_file.exists() else {}
        item = {
            "name": name, "version": version, "direct": name in direct,
            "license": package["license"], "source": archive,
            "checksum": checksums.get((name, version)), "repository": package["repository"],
            "commit": git_commit or vcs.get("git", {}).get("sha1"),
            "licenseFiles": [{"path": path, "sha256": hashlib.sha256(text.encode()).hexdigest(),
                              **({"source": url} if url else {})} for path, text, url in texts],
        }
        if vendor:
            item["vendoredPath"] = str(source.relative_to(ROOT))
            item["patch"] = vendor["patch"]
            item["modifiedFiles"] = {
                path: hashlib.sha256((source / path).read_bytes()).hexdigest()
                for path in vendor["modified-files"]
            }
        inventory.append(item)
        notices.append(f"\n{'=' * 78}\n{name} {version}\nLicense: {package['license']}\nSource: {archive}\n")
        if vendor:
            notices.append(f"Vendored: {item['vendoredPath']}\nPatch: {vendor['patch']} (see UPSTREAM.md)\n")
        for path, text, url in texts:
            rendered = "\n".join(line.rstrip() for line in text.splitlines()).rstrip()
            notices.append(f"\n--- {path}{' (' + url + ')' if url else ''} ---\n{rendered}\n")
    (ROOT / "LICENSES/rust-third-party.txt").write_text("".join(notices))
    (ROOT / "docs/dependencies.json").write_text(json.dumps({
        "formatVersion": 1, "application": "t3oxide-rust", "lockfile": "Cargo.lock",
        "scope": "complete locked graph, including platform-specific and build dependencies",
        "dependencies": inventory,
    }, indent=2) + "\n")
    print(f"Updated notices for {len(inventory)} locked upstream packages.")


if __name__ == "__main__":
    main()
