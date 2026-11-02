package ui

import (
	"encoding/json"
	"fmt"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/Xon333/t3oxide/internal/wire"
)

// No rendered Markdown is parsed to recover tool source. These transient offers
// retain the bounded decoded bytes and exact attachment/page/item identity.
type toolSourceOffer struct {
	Generation uint64
	Thread     string
	Cursor     string
	Historical bool
	Item       wire.ActivityItem
	Part       string
	Text       string
}
type toolSourceMsg struct {
	serial       uint64
	detailAtRead string
	offer        toolSourceOffer
	detail       wire.ActivityDetail
	err          error
}

func (m *Model) toolSourceCurrent(o toolSourceOffer) bool {
	if m.snapshot == nil || o.Generation != m.generation || (o.Thread != pageIdentity(m.snapshot).thread || o.Cursor != pageIdentity(m.snapshot).cursor || o.Historical != pageIdentity(m.snapshot).historical) || m.snapshot.Metadata.Activity == nil {
		return false
	}
	for _, item := range m.snapshot.Metadata.Activity.Items {
		if item == o.Item {
			return true
		}
	}
	return false
}

// copyToolSource("") offers loaded tool identities; a selection explicitly reads
// one bounded detail. The integrator routes copy-tool-source and toolSourceMsg.
func (m *Model) copyToolSource(args string) tea.Cmd {
	if m.snapshot == nil {
		return nil
	}
	if args == "" {
		items := []list.Item{}
		if page := m.snapshot.Metadata.Activity; page != nil {
			for _, item := range page.Items {
				o := toolSourceOffer{Generation: m.generation, Thread: pageIdentity(m.snapshot).thread, Cursor: pageIdentity(m.snapshot).cursor, Historical: pageIdentity(m.snapshot).historical, Item: item}
				raw, _ := json.Marshal(o)
				items = append(items, choiceItem{title: item.Title, description: item.Status, command: "copy-tool-source", args: string(raw)})
			}
		}
		m.openLocal("Copy tool source", "copy-tool-source", items)
		return nil
	}
	var offer toolSourceOffer
	if json.Unmarshal([]byte(args), &offer) != nil || !m.toolSourceCurrent(offer) {
		m.notice = "Tool source changed; select it again"
		return nil
	}
	if offer.Part != "" {
		if offer.Part != "Input" && offer.Part != "Result" {
			return nil
		}
		return m.copySourceBytes(redactSourceBytes(offer.Text))
	}
	if m.session == nil {
		return nil
	}
	session, ctx := m.session, m.ctx
	serial, detailAtRead := m.dialogSerial, m.detail
	return func() tea.Msg {
		d, err := session.ActivitySource(ctx, offer.Item)
		return toolSourceMsg{serial: serial, detailAtRead: detailAtRead, offer: offer, detail: d, err: err}
	}
}

func toolSourceParts(d wire.ActivityDetail) []toolSourceOffer {
	var parts []toolSourceOffer
	if d.Input != "" {
		parts = append(parts, toolSourceOffer{Part: "Input", Text: d.Input})
	}
	if d.OutputPresent {
		parts = append(parts, toolSourceOffer{Part: "Result", Text: d.Output})
	}
	return parts
}

func (m *Model) acceptToolSource(msg toolSourceMsg) tea.Cmd {
	if msg.serial != m.dialogSerial || msg.detailAtRead != m.detail || !m.toolSourceCurrent(msg.offer) {
		return nil
	}
	if msg.err != nil {
		m.problem = msg.err.Error()
		return nil
	}
	d := msg.detail
	if d.Key != msg.offer.Item.Key || d.SourceThreadID != msg.offer.Item.SourceThreadID || d.ItemID != msg.offer.Item.ItemID || d.Changed || d.RequestedRevision != msg.offer.Item.UpdatedAt {
		m.notice = "Tool source changed; select it again"
		return nil
	}
	items := []list.Item{}
	for _, part := range toolSourceParts(d) {
		offer := msg.offer
		offer.Part, offer.Text = part.Part, part.Text
		raw, _ := json.Marshal(offer)
		label := "Copy " + part.Part
		if d.Truncated || part.Part == "Result" && d.OutputOmitted {
			label += " · shortened source"
		}
		items = append(items, choiceItem{title: label, description: fmt.Sprintf("%d source bytes", len(part.Text)), command: "copy-tool-source", args: string(raw)})
	}
	m.openLocal("Copy tool Input / Result", "copy-tool-source", items)
	return nil
}
