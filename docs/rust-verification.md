# Rust implementation verification

Verified on **2026-11-02**. All recorded source, protocol and synthetic terminal gates passed. These results do not certify an installed user environment.

## Inputs and build identity

- Existing-client baseline: `e73caacd673d1a9bbceea8a7ed240b2a068ba5a7`.
- Reviewed T3 contracts: `ac8dce0cc3abd8a337f2d806b5082a6834b9b81a`; see [contract evidence](rust-contracts.md).
- Execution host: Linux x86_64; Rust/Cargo 1.99.1, Node 24.22.1, Python 3.12.14. The isolated baseline used Go 1.26.10.
- Rust release: `t3oxide 0.11.0 · Rust / Ratatui`, 3,324,744 bytes. SHA-256: `5f2554961bad5b942fae76c07dc4e26fa6e68c79cc9ef6579805d91b684a8721`.

Rust used the checked-in release profile (thin LTO, one code generation unit, stripped). The isolated unchanged baseline used `CGO_ENABLED=0`, `-trimpath`, `-ldflags='-s -w'`. The new application has no Go runtime.

## Completed gates

Commands, counts, binary identity and artifact hashes are in [checks.json](evidence/checks.json).

| Gate | Result |
| --- | --- |
| Rust formatting | `cargo fmt --all --check` passed |
| Clippy | All targets passed with `-D warnings` |
| Rust tests | **81 library + 3 integration** tests passed; [full log](evidence/rust-tests.log) |
| Companion and scripts | **237 Node + 5 Python** tests, syntax checks and compilation checks passed; [full log](evidence/adapter-and-scripts.log) |
| HTTP/WebSocket/stdio recovery | Four SIGKILL cut points for each of Stop, approval and question passed; [log](evidence/loopback.log) |
| Release and package | Linux release and packaged `dist` binary passed with identical SHA-256; [package log](evidence/packaged-build.log) |
| macOS source checks | All targets compile-checked for ARM64 and Intel; these are not Mac runtime, linking or signature checks |
| Real PTY acceptance | **41 recorded outcomes** passed on the release binary; [complete data](evidence/terminal-evidence.json) |

Real PTYs exercise both modes, control bytes, Unicode paste, mouse/resize, durable drafts/forms, external editing, requests, Stop, empty/messaged launches, model/options/overrides, history and host failure/reconnect. They also verify filtering, preview and server search without retargeting, loaded-text find/copy, local project/rank controls and partial task filing resumed through durable receipts.

Lost-acknowledgement recovery uses the original ID/payload with **one native effect**; disconnect/restart never resends automatically. Lifecycle tests cover newer identical drafts and late acknowledgements after source changes. PTYs exposed and corrected legacy Crossterm Ctrl+4/5 decoding for Ctrl+\/Ctrl+].

## Same-host resource comparison

Three fresh processes per client used the same synthetic native environment, active conversation, 140×38 terminal, three scheduled stream updates and 1.5-second idle window. Values below are **median (minimum–maximum)**. Tree RSS includes the Node companion.

| Measurement | Rust release | Pinned Go baseline |
| --- | ---: | ---: |
| Client RSS, KiB | 5,444 (5,412–5,684) | 29,004 (28,840–29,188) |
| Process-tree RSS, KiB | 47,768 (47,556–47,992) | 71,332 (71,176–71,380) |
| First transcript observed, ms | 156.84 (156.61–157.57) | 209.56 (208.94–213.52) |
| All three stream updates visible, ms | 516.71 (515.17–520.19) | 570.13 (569.01–623.12) |
| Idle client/tree CPU, ms | 0 / 0 (all samples) | 20 / 20 (all samples) |
| Idle terminal output, bytes | 0 (all samples) | 0 (all samples) |
| Live processes | 2 (all samples) | 2 (all samples) |
| Executable size, bytes | 3,324,744 | 15,941,794 |

In this workload, median client RSS was **81.2% lower**, and summed process-tree RSS was **33.0% lower**. These are bounded fixture measurements. RSS can count shared pages more than once; it is not macOS physical footprint. CPU resolution was 10 ms, so zero recorded ticks does not mean zero CPU work. Screen observations poll every 50 ms; filesystem caches were warm. Stream-ready time includes three intentional 100 ms fixture updates. These samples establish no physical paint, production provider, SSH, battery or large-history performance result.

## Reproduction and genuine host requirements

Reproduce with the commands in [contract evidence](rust-contracts.md#verification-fixtures); use `--baseline` for the isolated Go comparison. [Developer](evidence/developer.txt) and [AgentBox](evidence/agentbox.txt) captures contain synthetic data. These are terminal-text captures, not physical screenshots or paint measurements.

Installed authentication, actual SSH routes, provider execution and native terminal behavior still require the machines that own those resources. macOS target checks do not replace those checks. Updated companion files enable `inclusiveThreadList`; older protocol-compatible helpers may omit collapsed children from loaded metadata. Native server search remains separately available when advertised. No installed T3 service, productive thread, user configuration or user state was accessed or changed by these fixtures.
