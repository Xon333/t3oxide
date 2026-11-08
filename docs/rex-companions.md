# Experimental Rex companions

The companion demo keeps conversation A as the main interface. Its Inbox drawer
reuses B's pending-question list and offers the local demo review. Completed
outputs stay in the existing Library; B remains available with F2.

This is a reversible interaction demo. It does not replace the normal client or
migrate Library data. Enable it only with `T3OXIDE_DEMO_COMPANIONS` pointing to an
explicit trusted executable. The executable receives one bounded JSON request
on stdin and returns after creating a companion pane. Without that variable the
existing interface remains active.

## Available tools

| Action | Existing tool | Demo behavior |
|---|---|---|
| Graph / `/graph` | [Graf](https://github.com/reekta92/graf) | Browse, search, pan and zoom a private Markdown snapshot of loaded project/thread metadata. |
| Guide / `/guide` | [Glow](https://github.com/charmbracelet/glow) | Read the short Markdown demo guide. |
| Review / `/review` | Existing local image reviewer | View images and enter comments together; Enter saves/next, Ctrl+B back. |
| Diagram / `/mermaid` | [TermiFlow](https://github.com/dnvt/termiflow) CLI | View a supported Mermaid flowchart in the terminal. |
| Form / `/form` | [Inquire](https://github.com/mikaelmello/inquire) | Try an artificial select/multiselect/text/editor form; answers stay local. |

Agents should use these companion actions for this demo instead of rebuilding
their presentation inside A. The maintained Rex CLI places each view beside the
originating pane in the same tab using explicit inherited session/block IDs.
Keep the process host distinct from the display Mac. This first package runs on
its prepared M1 host; a local path is not a remote-host deployment contract.
Missing or uncertain placement must produce an error, not a guessed workspace or
an automatic retry. Interactive views close normally when the user exits them.

Graf receives explicit loaded membership and parent links, not message bodies,
inferred dependencies or a complete native graph. Its presentation is undirected.
It is browsing-only: selecting a graph node does not navigate A. Graf is a separate
GPL-3.0-only executable, with its corresponding source and license retained by
the demo package; no Graf code is linked into t3oxide. The prepared Inquire sample
and TermiFlow CLI are MIT licensed.

The image reviewer preserves its own comments, drafts, cursor and history. The
user finishes there and notifies the coordinator, who reads that exact saved
state. Neither it nor the artificial form sends native answers. Native questions
continue through the existing source/revision checks and single mutation owner.
Pane creation, program readiness, local save and native completion remain
separate facts. Closing a pane does not Stop a native run.

[Clin](https://github.com/reekta92/clin-rs) remains an unselected Library experiment.
Any experiment uses copied Markdown, not a Context migration or native authority.
Earlier A and reference annotations still apply to subsequent corrections.
