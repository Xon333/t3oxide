import AppKit

@main
@MainActor final class App: NSObject, NSApplicationDelegate, NSTableViewDataSource, NSTableViewDelegate, NSTextViewDelegate, NSWindowDelegate {
    private var window: NSWindow!
    private let connection = Connection()
    private let local = LocalState()
    private var environments: [Environment] = []
    private var threads: [[String: Any]] = []
    private var store = TranscriptStore()
    private var metadata: ViewMetadata? { store.snapshot?.metadata }
    private var displayOverride: ViewState?
    private var awaitingSnapshot = false
    private var activeRunTarget: String?
    private struct RenderedRow { let key: String; let text: String }
    private struct CachedRow { let kind: String; let text: String; let truncated: Bool; let attachments: Int; let rendered: String }
    private var rowCache: [String: CachedRow] = [:]
    private var renderedRows: [RenderedRow] = []
    private var displayState: ViewState { displayOverride ?? metadata?.state ?? .unavailable }
    private var normalMutationReady: Bool {
        ready && writable && !navigating && !awaitingSnapshot && !store.needsResnapshot &&
        displayState == .live && metadata?.live == true && metadata?.historical == false &&
        selectedThread != nil && metadata?.threadId == selectedThread
    }
    private var selectedThread: String?
    private var selectedEnvironment: Environment? { environments.indices.contains(environmentMenu.indexOfSelectedItem) ? environments[environmentMenu.indexOfSelectedItem] : nil }
    private var ready = false, writable = false, loadingList = false, reloading = false
    private var session = UUID(), navigation = UUID()
    private var navigating = false, resnapshotPending = false
    private var inFlight = Set<String>()
    private var sending: Bool { selectedEnvironment.flatMap { local.pending[$0.environmentId]?["id"] as? String }.map { inFlight.contains($0) } ?? false }
    private var stopping: Bool { selectedEnvironment.flatMap { local.pendingStops[$0.environmentId]?["id"] as? String }.map { inFlight.contains($0) } ?? false }
    private var draftSave: DispatchWorkItem?
    private var suspended = false
    private let environmentMenu = NSPopUpButton()
    private let search = NSSearchField()
    private let table = NSTableView()
    private let transcript = NSTextView()
    private let composer = NSTextView()
    private let status = NSTextField(wrappingLabelWithString: "")
    private let heading = NSTextField(labelWithString: "Select a conversation")
    private let delivery = NSPopUpButton()
    private var resolveButton: NSButton!, retryStopButton: NSButton!, resolveStopButton: NSButton!, latestButton: NSButton!
    private var sendButton: NSButton!, stopButton: NSButton!, requestButton: NSButton!, newButton: NSButton!, retryButton: NSButton!, olderButton: NSButton!
    private var draftKey: String?
    private var draftEnvironmentID: String?

    static func main() {
        let app = NSApplication.shared, delegate = App()
        app.delegate = delegate
        app.setActivationPolicy(.regular)
        withExtendedLifetime(delegate) { app.run() }
    }
    func applicationDidFinishLaunching(_ notification: Notification) {
        makeMenus(); makeWindow()
        connection.onPacket = { [weak self] packet in self?.packet(packet) }
        connection.onClosed = { [weak self] message in
            guard let self = self else { return }; self.ready = false; self.navigating = false; self.resnapshotPending = false; self.markUnavailable(); self.status.stringValue = message; self.updateControls()
        }
        NSWorkspace.shared.notificationCenter.addObserver(self, selector: #selector(sleepNow), name: NSWorkspace.willSleepNotification, object: nil)
        NSWorkspace.shared.notificationCenter.addObserver(self, selector: #selector(wakeNow), name: NSWorkspace.didWakeNotification, object: nil)
        do {
            environments = try EnvironmentsFile.load()
            environmentMenu.addItems(withTitles: environments.map(\.label))
            if environments.isEmpty { status.stringValue = "No enabled environments. Open Config to edit the prepared environment file." }
            else { connect() }
        } catch { status.stringValue = "Configuration needed at ~/.config/t3oxide/environments.json. Native setup instructions include the exact format." }
        window.makeKeyAndOrderFront(nil); NSApplication.shared.activate(ignoringOtherApps: true)
    }
    private func button(_ title: String, _ action: Selector) -> NSButton { NSButton(title: title, target: self, action: action) }
    private func stack(_ views: [NSView], vertical: Bool = false) -> NSStackView {
        let result = NSStackView(views: views); result.orientation = vertical ? .vertical : .horizontal
        result.alignment = vertical ? .leading : .centerY; result.spacing = 8
        return result
    }
    private func scroll(_ text: NSTextView, editable: Bool) -> NSScrollView {
        let scroller = NSScrollView(); scroller.hasVerticalScroller = true; scroller.borderType = .bezelBorder
        text.isEditable = editable; text.isSelectable = true; text.isRichText = false
        text.isAutomaticQuoteSubstitutionEnabled = false; text.isAutomaticDashSubstitutionEnabled = false
        text.isAutomaticTextReplacementEnabled = false; text.usesFindBar = !editable
        text.font = .systemFont(ofSize: 14); text.textContainerInset = NSSize(width: 12, height: 10)
        text.isVerticallyResizable = true; text.isHorizontallyResizable = false; text.autoresizingMask = [.width]
        text.minSize = NSSize(width: 0, height: 0); text.maxSize = NSSize(width: CGFloat.greatestFiniteMagnitude, height: CGFloat.greatestFiniteMagnitude)
        text.textContainer?.widthTracksTextView = true
        scroller.documentView = text; return scroller
    }
    private func makeMenus() {
        let menu = NSMenu(), appItem = NSMenuItem(), appMenu = NSMenu()
        appMenu.addItem(withTitle: "Quit t3oxide", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
        appItem.submenu = appMenu; menu.addItem(appItem)
        let editItem = NSMenuItem(title: "Edit", action: nil, keyEquivalent: ""), edit = NSMenu(title: "Edit")
        for (title, selector, key) in [("Undo", Selector(("undo:")), "z"), ("Cut", #selector(NSText.cut(_:)), "x"),
                                      ("Copy", #selector(NSText.copy(_:)), "c"), ("Paste", #selector(NSText.paste(_:)), "v"),
                                      ("Select All", #selector(NSText.selectAll(_:)), "a")] {
            edit.addItem(withTitle: title, action: selector, keyEquivalent: key)
        }
        editItem.submenu = edit; menu.addItem(editItem); NSApplication.shared.mainMenu = menu
    }
    private func makeWindow() {
        window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 900, height: 680),
                          styleMask: [.titled, .closable, .miniaturizable, .resizable], backing: .buffered, defer: false)
        window.title = "t3oxide"; window.minSize = NSSize(width: 780, height: 590); window.delegate = self; window.center()
        window.isReleasedWhenClosed = false
        environmentMenu.target = self; environmentMenu.action = #selector(changeEnvironment)
        search.placeholderString = "Filter threads · Return"; search.target = self; search.action = #selector(refreshList)
        let top = stack([environmentMenu, button("Reconnect", #selector(reconnect)), button("Open full T3", #selector(openT3)), button("Config", #selector(openConfig))])
        let column = NSTableColumn(identifier: NSUserInterfaceItemIdentifier("thread")); column.title = "Threads"; table.addTableColumn(column)
        table.headerView = nil; table.rowHeight = 53; table.delegate = self; table.dataSource = self
        table.usesAlternatingRowBackgroundColors = false; table.allowsEmptySelection = true
        let tableScroll = NSScrollView(); tableScroll.hasVerticalScroller = true; tableScroll.documentView = table
        let left = stack([search, tableScroll], vertical: true)
        let transcriptScroll = scroll(transcript, editable: false), composerScroll = scroll(composer, editable: true)
        composer.delegate = self
        heading.font = .boldSystemFont(ofSize: 14); heading.lineBreakMode = .byTruncatingTail
        olderButton = button("Earlier", #selector(earlier)); requestButton = button("Requests", #selector(answerRequest))
        newButton = button("New in same workspace", #selector(newThread))
        latestButton = button("Latest", #selector(latest))
        let history = stack([olderButton, latestButton, requestButton, newButton])
        delivery.addItems(withTitles: ["Send / auto", "Queue follow-up", "Steer active"])
        sendButton = button("Send", #selector(send)); sendButton.keyEquivalent = "\r"; sendButton.keyEquivalentModifierMask = [.command]
        stopButton = button("Stop active turn", #selector(stopTurn)); retryButton = button("Retry saved action", #selector(retryAction))
        resolveButton = button("Resolve saved action", #selector(resolveAction))
        let controls = stack([delivery, sendButton, stopButton])
        retryStopButton = button("Retry Stop", #selector(retryStop))
        resolveStopButton = button("Resolve Stop", #selector(resolveStop))
        let recovery = stack([retryButton, resolveButton, retryStopButton, resolveStopButton])
        let right = stack([heading, history, transcriptScroll, composerScroll, controls, recovery], vertical: true)
        let body = stack([left, right]); body.alignment = .top
        status.font = .systemFont(ofSize: 11); status.textColor = .secondaryLabelColor
        let root = stack([top, body, status], vertical: true)
        root.translatesAutoresizingMaskIntoConstraints = false
        window.contentView!.addSubview(root)
        for view in [top, body, status] { view.translatesAutoresizingMaskIntoConstraints = false; view.widthAnchor.constraint(equalTo: root.widthAnchor).isActive = true }
        for view in [search, tableScroll] { view.widthAnchor.constraint(equalTo: left.widthAnchor).isActive = true }
        for view in [heading, transcriptScroll, composerScroll] { view.widthAnchor.constraint(equalTo: right.widthAnchor).isActive = true }
        NSLayoutConstraint.activate([
            root.leadingAnchor.constraint(equalTo: window.contentView!.leadingAnchor, constant: 12),
            root.trailingAnchor.constraint(equalTo: window.contentView!.trailingAnchor, constant: -12),
            root.topAnchor.constraint(equalTo: window.contentView!.topAnchor, constant: 12),
            root.bottomAnchor.constraint(equalTo: window.contentView!.bottomAnchor, constant: -12),
            left.widthAnchor.constraint(equalToConstant: 215), left.heightAnchor.constraint(equalTo: body.heightAnchor),
            right.heightAnchor.constraint(equalTo: body.heightAnchor), right.widthAnchor.constraint(greaterThanOrEqualToConstant: 500),
            composerScroll.heightAnchor.constraint(equalToConstant: 110), transcriptScroll.heightAnchor.constraint(greaterThanOrEqualToConstant: 160),
            tableScroll.heightAnchor.constraint(greaterThanOrEqualToConstant: 200), status.heightAnchor.constraint(lessThanOrEqualToConstant: 48)
        ])
        updateControls()
    }
    @objc private func connect() {
        guard let env = selectedEnvironment, !suspended else { return }
        session = UUID(); navigation = UUID(); loadingList = false; navigating = false; resnapshotPending = false
        let epoch = session
        ready = false; writable = false; markUnavailable(); status.stringValue = "Connecting to \(env.label)…"; updateControls()
        do {
            try connection.start(env)
            connection.request("hello") { [weak self] result in
                guard let self = self, self.session == epoch, self.selectedEnvironment?.id == env.id else { return }
                self.consume(result) { value in
                    self.ready = true; self.writable = value["writesEnabled"] as? Bool == true && self.local.loadError == nil
                    self.status.stringValue = "\(env.label) · T3 \(value["serverVersion"] as? String ?? "")"
                    self.refreshList(); if self.selectedThread != nil { self.latest() }; self.updateControls()
                }
            }
        } catch { status.stringValue = error.localizedDescription }
    }
    @objc private func reconnect() { guard flushDraft() else { return }; connect() }
    @objc private func changeEnvironment() {
        // A draft is saved by its old connection identity, not the newly selected menu row.
        guard flushDraftForConnection() else {
            if let old = draftEnvironmentID, let index = environments.firstIndex(where: { $0.id == old }) { environmentMenu.selectItem(at: index) }
            return
        }
        selectedThread = nil; draftKey = nil; draftEnvironmentID = nil; clearView(); composer.string = ""; composer.undoManager?.removeAllActions()
        threads = []; table.reloadData(); heading.stringValue = "Select a conversation"; connect()
    }
    @objc private func refreshList() {
        guard ready, !loadingList else { return }; loadingList = true
        let env = selectedEnvironment?.id, epoch = session, selection = navigation
        connection.request("list", fields: ["query": search.stringValue]) { [weak self] result in
            guard let self = self, self.session == epoch else { return }; self.loadingList = false
            guard self.selectedEnvironment?.id == env else { return }
            if self.navigation != selection {
                switch result {
                case .failure: return
                case .success(let value): if value["error"] != nil { return }
                }
            }
            self.consume(result) { value in
                self.threads = value["threads"] as? [[String: Any]] ?? []
                self.reloading = true; self.table.reloadData()
                if let index = self.threads.firstIndex(where: { $0["id"] as? String == self.selectedThread }) { self.table.selectRowIndexes(IndexSet(integer: index), byExtendingSelection: false) }
                self.reloading = false
                if self.navigation == selection, value["more"] as? Bool == true { self.status.stringValue = "Showing the newest 200 matches. Narrow the filter for older threads." }
            }
        }
    }
    func numberOfRows(in tableView: NSTableView) -> Int { threads.count }
    func tableView(_ tableView: NSTableView, viewFor tableColumn: NSTableColumn?, row: Int) -> NSView? {
        guard threads.indices.contains(row) else { return nil }
        let item = threads[row], cell = NSTableCellView(), field = NSTextField(labelWithString: "")
        field.stringValue = "\(item["title"] as? String ?? "Untitled")\n\(item["project"] as? String ?? "") · \(item["status"] as? String ?? "")"
        field.font = .systemFont(ofSize: 12); field.maximumNumberOfLines = 2; field.lineBreakMode = .byTruncatingTail
        field.translatesAutoresizingMaskIntoConstraints = false; cell.addSubview(field); cell.textField = field
        NSLayoutConstraint.activate([field.leadingAnchor.constraint(equalTo: cell.leadingAnchor, constant: 6), field.trailingAnchor.constraint(equalTo: cell.trailingAnchor, constant: -6), field.centerYAnchor.constraint(equalTo: cell.centerYAnchor)])
        return cell
    }
    func tableViewSelectionDidChange(_ notification: Notification) {
        guard !reloading, threads.indices.contains(table.selectedRow), let id = threads[table.selectedRow]["id"] as? String else { return }
        guard flushDraft() else {
            reloading = true
            if let old = threads.firstIndex(where: { $0["id"] as? String == selectedThread }) { table.selectRowIndexes(IndexSet(integer: old), byExtendingSelection: false) }
            reloading = false; return
        }
        if selectedThread != id { selectThread(id) }
    }
    private func clearView() {
        store.reset(); rowCache.removeAll(); renderedRows.removeAll(); transcript.string = ""
        activeRunTarget = nil; awaitingSnapshot = false; resnapshotPending = false; displayOverride = nil
    }
    private func selectThread(_ id: String) {
        navigation = UUID(); navigating = false; selectedThread = id; clearView(); heading.stringValue = "Loading…"
        draftKey = selectedEnvironment.map { $0.environmentId + "/" + id }; draftEnvironmentID = selectedEnvironment?.id
        composer.string = draftKey.flatMap { local.drafts[$0] } ?? ""; composer.undoManager?.removeAllActions()
        latest(); updateControls()
    }
    private func markUnavailable() { displayOverride = .unavailable; render() }
    @objc private func latest() {
        guard ready, let id = selectedThread else { return }
        navigate("watch", fields: ["threadId": id])
    }
    @objc private func earlier() {
        guard ready, !navigating, !awaitingSnapshot, !store.needsResnapshot, let cursor = metadata?.historyCursor else { return }
        navigate("history", fields: ["cursor": cursor])
    }
    private func navigate(_ op: String, fields: [String: Any]) {
        navigation = UUID(); let selection = navigation, epoch = session, thread = selectedThread
        navigating = true; awaitingSnapshot = op == "watch"; displayOverride = .synchronizing
        render(); updateControls()
        // The adapter cancels the previous read when it admits a new watch/history request.
        connection.request(op, fields: fields) { [weak self] result in
            guard let self = self, self.session == epoch, self.navigation == selection, self.selectedThread == thread else { return }
            self.navigating = false
            var failure: String?
            switch result {
            case .failure(let error): failure = error.localizedDescription
            case .success(let value): failure = (value["error"] as? [String: Any])?["message"] as? String
            }
            if let failure = failure { self.markUnavailable(); self.status.stringValue = failure }
            self.updateControls()
        }
    }
    private func packet(_ packet: [String: Any]) {
        guard let event = packet["event"] as? String else { return }
        let data = packet["data"] as? [String: Any] ?? [:]
        if event == "view" || event == "delta" {
            guard selectedThread != nil else { return }
            if let thread = data["threadId"] as? String, thread != selectedThread { return }
            do {
                try store.apply(event: event, data: data)
                if event == "view" { awaitingSnapshot = false; resnapshotPending = false }
                if !awaitingSnapshot { displayOverride = ready ? nil : .unavailable }
                if let metadata = metadata, !metadata.historical {
                    if metadata.state == .live { activeRunTarget = metadata.activeRunId }
                    else if let run = metadata.activeRunId { activeRunTarget = run }
                }
                render(); updateControls()
            } catch {
                markUnavailable(); status.stringValue = "Conversation update could not be applied. Resynchronizing…"; updateControls()
                if ready, !resnapshotPending, let id = selectedThread {
                    resnapshotPending = true
                    navigate("watch", fields: ["threadId": id])
                }
            }
        } else if event == "status" {
            let state = data["state"] as? String ?? "unknown"
            if state == "connected" {
                ready = true
                writable = data["writesEnabled"] as? Bool == true && local.loadError == nil
            } else {
                if state != "unavailable" { ready = false }
                markUnavailable()
            }
            status.stringValue = data["message"] as? String ?? state
            updateControls()
        }
    }
    private func render() {
        guard let snapshot = store.snapshot else { return }
        let metadata = snapshot.metadata
        let state: String
        switch displayState {
        case .live: state = "LIVE"
        case .historical: state = "EARLIER HISTORY · Latest returns to active work"
        case .synchronizing: state = metadata.historical ? "SYNCHRONIZING · earlier history retained" : "SYNCHRONIZING · saved view retained"
        case .unavailable: state = metadata.historical ? "UNAVAILABLE · earlier history retained" : "UNAVAILABLE · saved view retained"
        }
        heading.stringValue = "\(metadata.title) · \(metadata.model) · \(state)"
        var next: [RenderedRow] = [], cache: [String: CachedRow] = [:]
        for row in snapshot.items {
            let rendered: String
            if let old = rowCache[row.key], old.kind == row.kind, (old.text as NSString).isEqual(to: row.text),
               old.truncated == row.truncated, old.attachments == row.attachmentCount { rendered = old.rendered }
            else {
                let label = row.kind == "user_message" ? "YOU" : row.kind == "assistant_message" ? "AGENT" : row.kind.replacingOccurrences(of: "_", with: " ").uppercased()
                var text = "\(label)\n\(row.text)"
                if row.truncated { text += "\n[Text shortened in this compact view. Open full T3 for the rest.]" }
                if row.attachmentCount > 0 { text += "\n[\(row.attachmentCount) attachment(s) available in full T3.]" }
                rendered = text + "\n\n"
            }
            cache[row.key] = CachedRow(kind: row.kind, text: row.text, truncated: row.truncated, attachments: row.attachmentCount, rendered: rendered)
            next.append(RenderedRow(key: "row:" + row.key, text: rendered))
        }
        next.append(RenderedRow(key: "footer", text: metadata.limited ? "[Display budget reached. Use Earlier to read preceding pages.]\n" : ""))
        updateTranscript(next); rowCache = cache
        status.stringValue = "\(selectedEnvironment?.label ?? "") · \(metadata.status) · \(state.lowercased())"
    }
    private func updateTranscript(_ next: [RenderedRow]) {
        // Stable rows keep their text storage. Edits to streaming rows trim both common ends.
        var prefix = 0, suffix = 0
        while prefix < min(renderedRows.count, next.count), renderedRows[prefix].key == next[prefix].key { prefix += 1 }
        while suffix < min(renderedRows.count, next.count) - prefix,
              renderedRows[renderedRows.count - suffix - 1].key == next[next.count - suffix - 1].key { suffix += 1 }
        var offsets = [0]
        for row in renderedRows { offsets.append(offsets.last! + (row.text as NSString).length) }
        var edits: [(NSRange, String)] = []
        func appendEdit(_ old: String, _ new: String, at offset: Int) {
            guard !(old as NSString).isEqual(to: new) else { return }
            let a = old as NSString, b = new as NSString
            var start = 0, end = 0
            while start < min(a.length, b.length), a.character(at: start) == b.character(at: start) { start += 1 }
            if start > 0 && start < a.length && (0xDC00...0xDFFF).contains(a.character(at: start)) { start -= 1 }
            while end < min(a.length, b.length) - start,
                  a.character(at: a.length - end - 1) == b.character(at: b.length - end - 1) { end += 1 }
            if end > 0 && (0xDC00...0xDFFF).contains(a.character(at: a.length - end)) { end -= 1 }
            edits.append((NSRange(location: offset + start, length: a.length - start - end), b.substring(with: NSRange(location: start, length: b.length - start - end))))
        }
        for i in 0..<prefix { appendEdit(renderedRows[i].text, next[i].text, at: offsets[i]) }
        if prefix + suffix < max(renderedRows.count, next.count) {
            appendEdit(renderedRows[prefix..<(renderedRows.count - suffix)].map(\.text).joined(),
                       next[prefix..<(next.count - suffix)].map(\.text).joined(), at: offsets[prefix])
        }
        for i in 0..<suffix {
            let oldIndex = renderedRows.count - suffix + i, newIndex = next.count - suffix + i
            appendEdit(renderedRows[oldIndex].text, next[newIndex].text, at: offsets[oldIndex])
        }
        renderedRows = next
        guard !edits.isEmpty, let storage = transcript.textStorage else { return }
        let visible = transcript.visibleRect, atBottom = visible.maxY >= transcript.bounds.maxY - 40
        let scrollOrigin = transcript.enclosingScrollView?.contentView.bounds.origin
        var selections = transcript.selectedRanges.map(\.rangeValue)
        // Anchor the top visible glyph as well as the selection when earlier text changes length.
        var anchor = 0, anchorY: CGFloat = 0
        if let layout = transcript.layoutManager, let container = transcript.textContainer, storage.length > 0 {
            let point = NSPoint(x: visible.minX - transcript.textContainerOrigin.x, y: visible.minY - transcript.textContainerOrigin.y)
            let glyph = layout.glyphIndex(for: point, in: container)
            if glyph < layout.numberOfGlyphs {
                anchor = layout.characterIndexForGlyph(at: glyph)
                anchorY = layout.lineFragmentRect(forGlyphAt: glyph, effectiveRange: nil).minY
            }
        }
        func adjusted(_ position: Int, _ range: NSRange, _ length: Int, trailing: Bool = true) -> Int {
            if position < range.location || (position == range.location && (!trailing || range.length > 0)) { return position }
            if position >= NSMaxRange(range) { return position + length - range.length }
            return range.location + min(position - range.location, length)
        }
        storage.beginEditing()
        for (range, replacement) in edits.reversed() {
            let length = (replacement as NSString).length
            selections = selections.map {
                let start = adjusted($0.location, range, length), end = $0.length == 0 ? start : adjusted(NSMaxRange($0), range, length, trailing: false)
                return NSRange(location: start, length: max(0, end - start))
            }
            anchor = adjusted(anchor, range, length)
            // Direct storage edits need explicit native attributes, including on
            // the first row; otherwise an empty view falls back to black text.
            storage.replaceCharacters(in: range, with: NSAttributedString(string: replacement, attributes: [
                .font: transcript.font ?? NSFont.systemFont(ofSize: 14), .foregroundColor: NSColor.textColor
            ]))
        }
        storage.endEditing()
        transcript.selectedRanges = selections.map { NSValue(range: $0) }
        if atBottom && selections.allSatisfy({ $0.length == 0 }) { transcript.scrollRangeToVisible(NSRange(location: storage.length, length: 0)) }
        else if let origin = scrollOrigin, let scroll = transcript.enclosingScrollView {
            var y = origin.y
            if let layout = transcript.layoutManager, let container = transcript.textContainer, storage.length > 0 {
                layout.ensureLayout(for: container)
                let glyph = layout.glyphIndexForCharacter(at: min(anchor, storage.length - 1))
                y += layout.lineFragmentRect(forGlyphAt: glyph, effectiveRange: nil).minY - anchorY
            }
            scroll.contentView.scroll(to: NSPoint(x: origin.x, y: max(0, y))); scroll.reflectScrolledClipView(scroll.contentView)
        }
    }
    private func updateControls() {
        let active = normalMutationReady && !sending
        let pending = selectedEnvironment.flatMap { local.pending[$0.environmentId] }
        let stopPending = selectedEnvironment.flatMap { local.pendingStops[$0.environmentId] }
        sendButton?.isEnabled = active && pending == nil
        stopButton?.isEnabled = ready && writable && !stopping && activeRunTarget != nil && selectedThread != nil && stopPending == nil && pending?["op"] as? String != "stop"
        let count = metadata?.requests.count ?? 0
        requestButton?.title = "Requests (\(count))"; requestButton?.isEnabled = active && count > 0 && pending == nil
        newButton?.isEnabled = active && pending == nil
        retryButton?.isHidden = pending == nil
        retryButton?.isEnabled = (pending?["op"] as? String == "stop" ? ready && writable && !sending && !stopping : active) && pending?["threadId"] as? String == selectedThread
        resolveButton?.isHidden = pending == nil; resolveButton?.isEnabled = pending != nil && !sending
        retryStopButton?.isHidden = stopPending == nil
        retryStopButton?.isEnabled = ready && writable && !stopping && selectedThread != nil && stopPending?["threadId"] as? String == selectedThread
        resolveStopButton?.isHidden = stopPending == nil; resolveStopButton?.isEnabled = stopPending != nil && !stopping
        olderButton?.isEnabled = ready && !navigating && !awaitingSnapshot && !store.needsResnapshot && metadata?.historyCursor != nil
        latestButton?.isEnabled = ready && selectedThread != nil
    }
    private func consume(_ result: Result<[String: Any], Error>, then action: ([String: Any]) -> Void) {
        switch result {
        case .failure(let error): status.stringValue = error.localizedDescription
        case .success(let value):
            if let error = value["error"] as? [String: Any] { status.stringValue = error["message"] as? String ?? "Request rejected." }
            else { action(value) }
        }
    }
    func textView(_ textView: NSTextView, shouldChangeTextIn affectedCharRange: NSRange, replacementString: String?) -> Bool {
        guard textView === composer, let replacement = replacementString else { return true }
        let original = composer.string as NSString
        guard affectedCharRange.location <= original.length && affectedCharRange.length <= original.length - affectedCharRange.location else { return false }
        let candidate = original.replacingCharacters(in: affectedCharRange, with: replacement)
        guard candidate.utf8.count <= 64 * 1024 else { status.stringValue = "Draft exceeds 64 KiB; keep larger material on the execution host."; return false }
        return true
    }
    func textDidChange(_ notification: Notification) {
        guard notification.object as? NSTextView === composer else { return }
        draftSave?.cancel(); let task = DispatchWorkItem { [weak self] in self?.flushDraft() }
        draftSave = task; DispatchQueue.main.asyncAfter(deadline: .now() + 0.5, execute: task)
    }
    @discardableResult private func flushDraftForConnection() -> Bool { flushDraft() }
    @discardableResult private func flushDraft() -> Bool {
        draftSave?.cancel(); guard let key = draftKey else { return true }
        do { try local.setDraft(composer.string, key: key); return true }
        catch { status.stringValue = error.localizedDescription; return false }
    }
    @objc private func send() {
        guard let id = selectedThread, flushDraft() else { return }
        perform("send", fields: ["threadId": id, "message": composer.string, "mode": ["auto", "queue", "steer"][delivery.indexOfSelectedItem]])
    }
    @objc private func stopTurn() {
        guard let id = selectedThread, let run = activeRunTarget else { return }
        perform("stop", fields: ["threadId": id, "runId": run])
    }
    private func perform(_ op: String, fields: [String: Any], savedID: String? = nil, savedStopLane: Bool? = nil) {
        let isStop = op == "stop", stopLane = savedStopLane ?? (op == "stop")
        guard ready, writable, let env = selectedEnvironment, let thread = selectedThread,
              fields["threadId"] as? String == thread,
              isStop ? !stopping : normalMutationReady && !sending else { return }
        let previous = stopLane ? local.pendingStops[env.environmentId] : local.pending[env.environmentId]
        guard previous == nil || previous?["id"] as? String == savedID else { return }
        let id = savedID ?? UUID().uuidString, epoch = session, selection = navigation
        guard !inFlight.contains(id) else { return }
        var saved = fields; saved["id"] = id; saved["op"] = op; saved["environmentId"] = env.environmentId
        if stopLane { local.pendingStops[env.environmentId] = saved } else { local.pending[env.environmentId] = saved }
        do { try local.save() } catch {
            if stopLane { local.pendingStops[env.environmentId] = previous } else { local.pending[env.environmentId] = previous }
            status.stringValue = "Could not save operation identity; nothing sent."; return
        }
        inFlight.insert(id); updateControls()
        connection.request(op, fields: fields, id: id) { [weak self] result in
            guard let self = self else { return }; self.inFlight.remove(id)
            let visible = self.session == epoch && self.navigation == selection && self.selectedEnvironment?.id == env.id
            var acknowledged = false, accepted: [String: Any]?
            switch result {
            case .failure(let error): if visible { self.status.stringValue = error.localizedDescription }
            case .success(let value):
                if let error = value["error"] as? [String: Any] {
                    acknowledged = savedID == nil && error["uncertain"] as? Bool != true
                    if visible {
                        let message = error["message"] as? String ?? "Action rejected."
                        self.status.stringValue = savedID == nil ? message : "\(message) Earlier acceptance remains unconfirmed. Inspect full T3, then resolve the saved action."
                    }
                } else { acknowledged = true; accepted = value }
            }
            if acknowledged {
                if stopLane { self.local.pendingStops.removeValue(forKey: env.environmentId) }
                else { self.local.pending.removeValue(forKey: env.environmentId) }
                do { try self.local.save() } catch {
                    // Keep the exact ID retryable if recording the acknowledgement failed.
                    if stopLane { self.local.pendingStops[env.environmentId] = saved } else { self.local.pending[env.environmentId] = saved }
                    if visible { self.status.stringValue = "Action acknowledged, but its local record could not be cleared. Retry uses the same ID." }
                    self.updateControls(); return
                }
            }
            if let value = accepted {
                if op == "send", let thread = fields["threadId"] as? String, let message = fields["message"] as? String {
                    let key = env.environmentId + "/" + thread
                    if self.local.drafts[key] == message { self.local.drafts.removeValue(forKey: key) }
                    if self.selectedEnvironment?.id == env.id && self.selectedThread == thread && self.composer.string == message {
                        self.composer.string = ""; self.composer.undoManager?.removeAllActions()
                    }
                    do { try self.local.save() } catch { if visible { self.status.stringValue = error.localizedDescription } }
                }
                if visible {
                    self.status.stringValue = "Accepted by \(env.label)."
                    if op == "launch", let thread = value["threadId"] as? String, self.flushDraft() {
                        self.selectThread(thread)
                    }
                    self.refreshList()
                }
            }
            self.updateControls()
        }
    }
    @objc private func resolveAction() { resolveSavedAction(isStop: false) }
    @objc private func resolveStop() { resolveSavedAction(isStop: true) }
    private func resolveSavedAction(isStop: Bool) {
        guard let env = selectedEnvironment, !(isStop ? stopping : sending),
              let saved = isStop ? local.pendingStops[env.environmentId] : local.pending[env.environmentId] else { return }
        let alert = NSAlert(); alert.messageText = isStop ? "Resolve an unconfirmed Stop" : "Resolve an unconfirmed action"
        alert.informativeText = "First inspect the conversation in full T3. This clears only the saved local retry record; it does not cancel, retry, or undo any server work. Your draft is retained."
        alert.addButton(withTitle: "Keep saved action"); alert.addButton(withTitle: "Clear local record")
        if alert.runModal() == .alertSecondButtonReturn {
            let currentRecord = isStop ? local.pendingStops[env.environmentId] : local.pending[env.environmentId]
            guard currentRecord?["id"] as? String == saved["id"] as? String else { return }
            if isStop { local.pendingStops.removeValue(forKey: env.environmentId) } else { local.pending.removeValue(forKey: env.environmentId) }
            do { try local.save(); status.stringValue = "Local retry record cleared; no server action sent." }
            catch {
                if isStop { local.pendingStops[env.environmentId] = saved } else { local.pending[env.environmentId] = saved }
                status.stringValue = error.localizedDescription
            }
            updateControls()
        }
    }
    @objc private func retryAction() { retrySavedAction(isStop: false) }
    @objc private func retryStop() { retrySavedAction(isStop: true) }
    private func retrySavedAction(isStop: Bool) {
        guard let env = selectedEnvironment, var saved = isStop ? local.pendingStops[env.environmentId] : local.pending[env.environmentId],
              saved["environmentId"] as? String == env.environmentId,
              let id = saved.removeValue(forKey: "id") as? String, let op = saved.removeValue(forKey: "op") as? String,
              isStop ? op == "stop" : ["send", "stop", "launch", "answer"].contains(op) else { return }
        saved.removeValue(forKey: "environmentId"); perform(op, fields: saved, savedID: id, savedStopLane: isStop)
    }
    @objc private func newThread() {
        guard normalMutationReady, let id = selectedThread else { return }
        let epoch = session, selection = navigation
        let alert = NSAlert(); alert.messageText = "New conversation in this workspace"
        alert.informativeText = "Uses this thread’s project, checkout, model and permission mode. Does not copy conversation history or create a new worktree."
        let title = NSTextField(string: ""), prompt = NSTextField(wrappingLabelWithString: "")
        title.placeholderString = "Conversation title"; prompt.isEditable = true; prompt.isSelectable = true; prompt.isBezeled = true
        prompt.placeholderString = "First message"; prompt.preferredMaxLayoutWidth = 420
        let content = stack([title, prompt], vertical: true); content.frame = NSRect(x: 0, y: 0, width: 440, height: 110)
        title.widthAnchor.constraint(equalToConstant: 430).isActive = true; prompt.widthAnchor.constraint(equalToConstant: 430).isActive = true
        prompt.heightAnchor.constraint(equalToConstant: 75).isActive = true
        alert.accessoryView = content; alert.addButton(withTitle: "Create"); alert.addButton(withTitle: "Cancel")
        if alert.runModal() == .alertFirstButtonReturn, session == epoch, navigation == selection { perform("launch", fields: ["threadId": id, "title": title.stringValue, "message": prompt.stringValue]) }
    }
    @objc private func answerRequest() {
        guard normalMutationReady, let thread = selectedThread else { return }
        let epoch = session, selection = navigation
        let requests = metadata?.requests ?? []
        guard !requests.isEmpty else { return }
        let picker = NSAlert(); picker.messageText = "Pending requests"
        let menu = NSPopUpButton(frame: NSRect(x: 0, y: 0, width: 430, height: 28))
        menu.addItems(withTitles: requests.map { String(($0["prompt"] as? String ?? $0["kind"] as? String ?? "Request").prefix(100)) })
        picker.accessoryView = menu; picker.addButton(withTitle: "Review"); picker.addButton(withTitle: "Cancel")
        guard picker.runModal() == .alertFirstButtonReturn, session == epoch, navigation == selection else { return }
        let request = requests[menu.indexOfSelectedItem]
        guard let id = request["id"] as? String, let revision = request["revision"] as? String else { return }
        guard request["supported"] as? Bool == true else { status.stringValue = request["limitation"] as? String ?? "Use full T3 for this request."; return }
        let alert = NSAlert(); alert.messageText = "Review request"
        alert.informativeText = request["prompt"] as? String ?? ""
        var fields: [String: Any] = ["threadId": thread, "requestId": id, "revision": revision]
        if request["kind"] as? String == "approval_request" {
            let options = request["options"] as? [[String: Any]] ?? []
            guard !options.isEmpty, options.count <= 5 else { status.stringValue = "Review this approval in full T3."; return }
            for option in options {
                alert.addButton(withTitle: option["label"] as? String ?? "Option")
                if let warning = option["warning"] as? String { alert.informativeText += "\n\n\(warning)" }
            }
            alert.addButton(withTitle: "Back")
            let index = alert.runModal().rawValue - NSApplication.ModalResponse.alertFirstButtonReturn.rawValue
            guard options.indices.contains(index), session == epoch, navigation == selection else { return }
            fields["decision"] = options[index]["decision"]; perform("answer", fields: fields)
        } else {
            let questions = request["questions"] as? [[String: Any]] ?? []
            guard questions.count <= 8 else { status.stringValue = "Review this question set in full T3."; return }
            let form = NSStackView(); form.orientation = .vertical; form.alignment = .leading; form.spacing = 10
            var controls: [(String, () -> Any?)] = []
            for question in questions {
                guard let qid = question["id"] as? String else { continue }
                let label = NSTextField(wrappingLabelWithString: question["question"] as? String ?? qid)
                label.preferredMaxLayoutWidth = 410; form.addArrangedSubview(label)
                let options = question["options"] as? [[String: Any]] ?? []
                guard options.count <= 24 else { status.stringValue = "Review this large option list in full T3."; return }
                let custom = NSTextField(string: question["initialAnswer"] as? String ?? "")
                custom.placeholderString = "Your answer"; custom.widthAnchor.constraint(equalToConstant: 400).isActive = true
                if question["multiSelect"] as? Bool == true && !options.isEmpty {
                    let checks = options.map { option -> NSButton in
                        let b = NSButton(checkboxWithTitle: option["label"] as? String ?? "", target: nil, action: nil); form.addArrangedSubview(b); return b
                    }
                    if question["allowCustomAnswer"] as? Bool == true { form.addArrangedSubview(custom) }
                    controls.append((qid, {
                        var chosen = options.enumerated().compactMap { checks[$0.offset].state == .on ? ($0.element["value"] as? String ?? $0.element["label"] as? String ?? "") : nil }
                        if question["allowCustomAnswer"] as? Bool == true && !custom.stringValue.isEmpty { chosen.append(custom.stringValue) }
                        return chosen.isEmpty ? nil : chosen
                    }))
                } else if !options.isEmpty {
                    let choices = NSPopUpButton(); choices.addItem(withTitle: "Choose…"); choices.addItems(withTitles: options.map { $0["label"] as? String ?? "" }); form.addArrangedSubview(choices)
                    if question["allowCustomAnswer"] as? Bool == true { form.addArrangedSubview(custom) }
                    controls.append((qid, {
                        if question["allowCustomAnswer"] as? Bool == true && !custom.stringValue.isEmpty { return custom.stringValue }
                        let n = choices.indexOfSelectedItem - 1
                        return options.indices.contains(n) ? (options[n]["value"] as? String ?? options[n]["label"] as? String) : nil
                    }))
                } else {
                    form.addArrangedSubview(custom); controls.append((qid, { custom.stringValue }))
                }
            }
            form.layoutSubtreeIfNeeded(); form.frame = NSRect(x: 0, y: 0, width: 430, height: max(70, form.fittingSize.height))
            let scroller = NSScrollView(frame: NSRect(x: 0, y: 0, width: 450, height: min(360, form.frame.height)))
            scroller.hasVerticalScroller = true; scroller.documentView = form; alert.accessoryView = scroller
            alert.addButton(withTitle: "Answer"); alert.addButton(withTitle: "Cancel")
            guard alert.runModal() == .alertFirstButtonReturn, session == epoch, navigation == selection else { return }
            var answers: [String: Any] = [:]; for (key, get) in controls { if let value = get() { answers[key] = value } }
            fields["answers"] = answers; perform("answer", fields: fields)
        }
    }
    @objc private func openT3() {
        guard let link = selectedEnvironment?.webURL, let url = URL(string: link) else { status.stringValue = "Set webURL in the environment file to open full T3."; return }
        NSWorkspace.shared.open(url)
    }
    @objc private func openConfig() {
        if FileManager.default.fileExists(atPath: EnvironmentsFile.url.path) { NSWorkspace.shared.open(EnvironmentsFile.url) }
        else { status.stringValue = "Create ~/.config/t3oxide/environments.json from config/environments.example.json, then relaunch." }
    }
    private func suspend() { flushDraft(); suspended = true; session = UUID(); navigation = UUID(); ready = false; navigating = false; markUnavailable(); connection.close(); updateControls() }
    @objc private func sleepNow() { suspend() }
    @objc private func wakeNow() { if !NSApplication.shared.isHidden && !window.isMiniaturized { suspended = false; connect() } }
    func applicationDidHide(_ notification: Notification) { suspend() }
    func applicationDidUnhide(_ notification: Notification) { suspended = false; connect() }
    func windowDidMiniaturize(_ notification: Notification) { suspend() }
    func windowDidDeminiaturize(_ notification: Notification) { suspended = false; connect() }
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { true }
    func windowShouldClose(_ sender: NSWindow) -> Bool { flushDraft() }
    func applicationShouldTerminate(_ sender: NSApplication) -> NSApplication.TerminateReply { flushDraft() ? .terminateNow : .terminateCancel }
    func applicationWillTerminate(_ notification: Notification) { flushDraft(); connection.close() }
}
