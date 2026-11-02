# Dependency notices

`Cargo.lock` pins the Rust dependency graph. [rust-third-party.txt](rust-third-party.txt)
contains the original license and notice texts for all locked upstream packages.
[The source inventory](../docs/dependencies.json) records exact versions, archive
checksums, source URLs and source commits where published by upstream crates.

The application directly reuses unmodified Ratatui, Crossterm, Ratatui textarea,
tui-markdown and nucleo-matcher libraries. It does not vendor their code. The
MPL-2.0 matcher is used as an unmodified library; its corresponding source archive
is https://crates.io/api/v1/crates/nucleo-matcher/0.3.1/download. Its license is
included in the combined text and its checksum in the lockfile and inventory.

Run `python3 scripts/update-rust-notices.py` after an intentional dependency
update and review the resulting inventory. `rust-supplements.json` supplies
upstream license texts for platform-support crates whose published package
omits a standalone license file. The generator validates locked checksums.

The retained native adapter adaptations have T3, Effect and t3term attribution
in the root [NOTICE](../NOTICE); the t3term MIT license is also retained here.
Include NOTICE and this directory with binary distributions. These third-party
notices do not grant a blanket license to t3oxide's original application code.
