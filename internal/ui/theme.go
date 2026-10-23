package ui

import (
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
	"github.com/Xon333/t3oxide/internal/client"
)

// The three highlight slots keep their roles across themes: accent owns context
// and selection, secondary owns links/inline code, operation owns run status.
// Warning and error remain independent semantic states.
type palette struct{ name, accent, secondary, operation, fg, muted, bg, elevated, error, warning string }

func colors(name string) palette {
	if name == "terminal" {
		// Empty base colors inherit the terminal defaults. ANSI accents resolve
		// through its palette, including a Ghostty theme's configured colors.
		return palette{name: name, accent: "4", secondary: "6", operation: "5", error: "1", warning: "3"}
	}
	base, variant, derived := environmentVariant(name)
	if derived {
		name = base
	}
	p := palette{name: name, fg: "#E6E9F0", muted: "#A7ADBF", bg: "#20232B", elevated: "#2A2F3A", error: "#FF8298", warning: "#EBCB8B"}
	switch name {
	case "cyan":
		p.accent, p.secondary, p.operation = "#84D7E4", "#C1D99B", "#BBA5FA"
	case "lime":
		p.accent, p.secondary, p.operation = "#C1D99B", "#BBA5FA", "#84D7E4"
	default:
		p.accent, p.secondary, p.operation = "#BBA5FA", "#84D7E4", "#C1D99B"
	}
	if derived {
		// Preserve the existing derived context/selection sequence. When it
		// takes another slot's color, that slot receives the now-unused base
		// accent. Roles stay fixed and all three highlights remain distinct.
		accents := [...]string{"#BBA5FA", "#84D7E4", "#C1D99B", "#F0ADCA", "#90B9F5", "#E8B590", "#81D3B5", "#D9AFE1"}
		offset := 0
		if name == "cyan" {
			offset = 1
		} else if name == "lime" {
			offset = 2
		}
		baseAccent := p.accent
		p.accent = accents[(variant+offset)%len(accents)]
		if p.accent == p.secondary {
			p.secondary = baseAccent
		}
		if p.accent == p.operation {
			p.operation = baseAccent
		}
	}
	return p
}
func (p palette) plain() lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(p.fg)) }
func (p palette) subtle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(p.muted))
}
func (p palette) active() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(p.accent)).Bold(true)
}

// operational marks run/responding/completion; it is never selection or warning.
func (p palette) operational() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(p.operation)).Bold(true)
}

func (p palette) selection() lipgloss.Style {
	if p.name == "terminal" {
		// Reverse the user's default pair rather than guessing light/dark RGB.
		return p.plain().Reverse(true).Bold(true)
	}
	return p.active().Background(lipgloss.Color(p.elevated))
}

func (p palette) highlight() lipgloss.Style {
	if p.name == "terminal" {
		return p.selection()
	}
	return lipgloss.NewStyle().Background(lipgloss.Color(p.elevated))
}

func (m *Model) applyTheme() {
	p := colors(m.theme)
	styles := textarea.DefaultDarkStyles()
	styles.Focused.Text = p.plain()
	styles.Focused.Prompt = p.active()
	styles.Focused.Placeholder = p.subtle()
	styles.Focused.CursorLine = lipgloss.NewStyle()
	styles.Focused.Base = lipgloss.NewStyle()
	if p.name == "terminal" {
		styles.Focused.Selection = p.selection()
		styles.Focused.LineNumber = p.subtle()
		styles.Focused.CursorLineNumber = p.subtle()
		styles.Focused.EndOfBuffer = p.subtle()
	}
	styles.Blurred = styles.Focused
	styles.Blurred.Prompt = p.subtle()
	styles.Cursor.Color = lipgloss.Color(p.accent)
	styles.Cursor.Blink = false
	m.composer.SetStyles(styles)
	m.modalInput.SetStyles(styles)
	search := textinput.DefaultStyles(true)
	search.Focused.Prompt = p.active()
	search.Focused.Text = p.plain()
	search.Focused.Placeholder = p.subtle()
	if p.name == "terminal" {
		search.Focused.Suggestion = p.subtle()
	}
	search.Blurred = search.Focused
	search.Cursor.Color = lipgloss.Color(p.accent)
	search.Cursor.Blink = false
	m.searchInput.SetStyles(search)
	m.viewport.HighlightStyle = p.highlight()
	if m.dialog != nil && m.dialog.kind != client.EnterText {
		m.styleList(&m.dialog.list)
	}
}

func (m *Model) styleList(l *list.Model) {
	p := colors(m.theme)
	l.Styles = list.DefaultStyles(true)
	d := list.NewDefaultDelegate()
	d.SetSpacing(0)
	d.ShowDescription = false
	d.SetHeight(1)
	d.Styles.NormalTitle = p.plain().PaddingLeft(2)
	d.Styles.NormalDesc = p.subtle().PaddingLeft(2)
	d.Styles.SelectedTitle = p.selection().Border(lipgloss.NormalBorder(), false, false, false, true).BorderForeground(lipgloss.Color(p.accent)).PaddingLeft(1)
	d.Styles.SelectedDesc = d.Styles.SelectedTitle.Bold(false)
	if p.name != "terminal" {
		d.Styles.SelectedDesc = d.Styles.SelectedDesc.Foreground(lipgloss.Color(p.secondary))
	}
	d.Styles.DimmedTitle = d.Styles.NormalTitle
	d.Styles.DimmedDesc = d.Styles.NormalDesc
	d.Styles.FilterMatch = lipgloss.NewStyle().Foreground(lipgloss.Color(p.secondary)).Underline(true)
	l.SetDelegate(compactChoiceDelegate{d})
	l.Styles.Title = p.active()
	l.Styles.TitleBar = lipgloss.NewStyle().PaddingLeft(1)
	l.Styles.NoItems = p.subtle().PaddingLeft(1)
	l.Styles.StatusBar = p.subtle().PaddingLeft(1)
	l.Styles.PaginationStyle = p.subtle().PaddingLeft(1)
	l.Styles.HelpStyle = p.subtle()
	l.Styles.Filter = textinput.DefaultStyles(true)
	l.Styles.Filter.Focused.Text = p.plain()
	l.Styles.Filter.Focused.Prompt = p.active()
	l.Styles.Filter.Focused.Placeholder = p.subtle()
	if p.name == "terminal" {
		l.Styles.Filter.Focused.Suggestion = p.subtle()
		l.Styles.Filter.Blurred = l.Styles.Filter.Focused
		l.Styles.StatusEmpty = p.subtle()
		l.Styles.StatusBarActiveFilter = p.plain()
		l.Styles.StatusBarFilterCount = p.subtle()
		l.Styles.ArabicPagination = p.subtle()
		l.Styles.ActivePaginationDot = p.active().SetString("●")
		l.Styles.InactivePaginationDot = p.subtle().SetString("○")
		l.Styles.DividerDot = p.subtle().SetString(" · ")
		l.Styles.Spinner = p.active()
	}
	l.Paginator.ActiveDot = l.Styles.ActivePaginationDot.String()
	l.Paginator.InactiveDot = l.Styles.InactivePaginationDot.String()
	l.Styles.Filter.Cursor.Color = lipgloss.Color(p.accent)
	l.Styles.Filter.Cursor.Blink = false
	l.FilterInput.SetStyles(l.Styles.Filter)
}
