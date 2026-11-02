package ui

import (
	"strings"
	"testing"

	"github.com/Xon333/t3oxide/internal/wire"
	"github.com/charmbracelet/x/ansi"
)

func TestOwnerPaletteShowsShortcutsWithoutSelectedProse(t *testing.T) {
	for _, mode := range []string{"developer", "agentbox"} {
		m := testModel(t)
		m.mode = mode
		m.openPalette()
		if footer := m.pickerSelectionView(80); footer != "" {
			t.Fatalf("%s selected prose: %q", mode, footer)
		}
		found := false
		for index, raw := range m.dialog.list.Items() {
			item := raw.(choiceItem)
			if item.shortcut == "" {
				continue
			}
			found = true
			m.dialog.list.Select(index)
			m.refreshDialogDetails()
			if !strings.Contains(m.dialog.detailText, item.description) {
				t.Fatal("Info lost full command description")
			}
			if !strings.Contains(ansi.Strip(m.dialog.list.View()), item.shortcut) {
				t.Fatalf("shortcut missing for %s", item.title)
			}
			break
		}
		if !found {
			t.Fatal("palette has no shortcuts")
		}
	}
}

func TestOwnerProviderHeaderPreservesExactIdentity(t *testing.T) {
	m := testModel(t)
	m.status.ModelName = "model-id"
	m.snapshot = &wire.Snapshot{Metadata: wire.Metadata{ProviderIdentity: &wire.ProviderIdentity{Label: "Pro", InstanceID: "provider-instance", AccountLabel: "reported-account"}}}
	if !strings.Contains(m.conversationDetailText(), "reported-account") {
		t.Fatal("Info lost account identity")
	}
	for _, want := range []string{"Pro", "provider-instance", "model-id"} {
		if !strings.Contains(m.providerModelStatus(), want) {
			t.Fatalf("header omitted %s", want)
		}
	}
}
