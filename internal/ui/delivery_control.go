package ui

import (
	"strings"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
)

func (m *Model) busyComposer() bool {
	return m.status.ActiveRunID != "" || m.status.RunState == "running" || m.status.RunState == "active" || m.status.RunState == "responding"
}
func (m *Model) selectedDelivery() string {
	if m.nextDelivery != "" {
		return m.nextDelivery
	}
	if m.status.Delivery != "" {
		return m.status.Delivery
	}
	return "auto"
}
func (m *Model) deliveryLabel() string {
	label := m.selectedDelivery()
	label = strings.ToUpper(label[:1]) + label[1:]
	if m.nextDelivery != "" {
		label += " (next)"
	}
	if m.selectedDelivery() == "auto" {
		label += " · T3 decides"
	}
	return label
}
func (m *Model) openDeliveryMenu() tea.Cmd {
	items := []list.Item{}
	for _, mode := range []string{"auto", "queue", "steer"} {
		label := strings.ToUpper(mode[:1]) + mode[1:]
		if mode == m.selectedDelivery() {
			label = "● " + label
		}
		description := "Stage this message's intent; Send submits it"
		if mode == "auto" {
			description = "T3 decides; no queue/steer outcome assumed"
		}
		items = append(items, choiceItem{title: label, description: description, command: "next-delivery", args: mode})
	}
	items = append(items, choiceItem{title: "Use saved delivery", description: "Saved: " + m.status.Delivery, command: "next-delivery", args: "clear"})
	m.openLocal("Next send delivery", "delivery", items)
	return nil
}

func (m *Model) stageDelivery(mode string) tea.Cmd {
	if mode == "clear" {
		m.nextDelivery = ""
	} else if mode == "auto" || mode == "queue" || mode == "steer" {
		m.nextDelivery = mode
	} else {
		return nil
	}
	m.deliverySerial++
	m.dialog = nil
	m.notice = "Next send: " + m.deliveryLabel() + " · saved delivery unchanged"
	m.resize()
	return m.restoreFocus()
}
