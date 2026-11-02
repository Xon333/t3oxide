package ui

import "testing"

func TestBrowseScopeCommandOffer(t *testing.T) {
	items := BrowseScopeItems()
	if len(items) != 1 {
		t.Fatal("missing browse offer")
	}
	item := items[0].(choiceItem)
	if item.command != "browse-scope" || item.args != "" {
		t.Fatalf("wrong browse target: %+v", item)
	}
	var m Model
	handled, cmd := m.dispatchBrowseSlice("new", "")
	if handled || cmd != nil {
		t.Fatal("browse helper captured New")
	}
	handled, cmd = m.dispatchBrowseSlice("browse-scope", "")
	if !handled || cmd != nil {
		t.Fatal("missing safe session guard")
	}
}
