# Deferred Octopus comparison for t3oxide

Backlog for Otis. Reuse existing SLT and native T3 surfaces. These comparison and acceptance cases still need t3oxide verification. Keep desktop x3code polish and launch ideas in its existing backlog.

- [ ] **Command discovery.** Compare the existing slash completion and action catalog with Octopus’s inline descriptions. Verify what helps users discover actions and distinguish inserting a command for editing from invoking it. Preserve native keybindings.
- [ ] **Draft and paste.** In native Rex, verify that multiline text and pasted slash-prefixed text remain literal and unsent, that cancelling command selection preserves the draft, and that the send hint matches the keys Rex delivers. Preserve plain Enter for newlines and keep drafts independent.
- [ ] **Acknowledgement and completion.** Compare how clearly visible status distinguishes submission admitted, work in progress, required operator response, Stop acknowledged while current work winds down, recorded settlement, and uncertainty after disconnect. Preserve native requests, operation receipts and recovery, and Stop. Where relevant, include reopening the same native thread after detach as an acceptance case.

## Reference evidence

Octopus revision `68fd20dc7432909f964f9378b52b71b508e0185f`. The desktop image was author-supplied. The terminal images used the pinned renderer in an offline fixture. Typed slash opened the picker, and multiline slash paste stayed an unsent draft. Source inspection verified Stop, admission, and recorded completion. Live execution and continuity remain untested.
