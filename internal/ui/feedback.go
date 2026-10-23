package ui

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Error detail reuses the existing viewport and the complete retained errors.
// It never reads host configuration or serializes model options/credentials.
func (m *Model) feedbackDetailText() string {
	if m.problem == "" && m.streamProblem == "" {
		return ""
	}
	diagnosis, next := m.feedbackDiagnosis()
	parts := []string{diagnosis}
	var target []string
	for _, field := range []struct{ label, value string }{
		{"Profile", m.status.ProfileID},
		{"Environment", m.status.EnvironmentID},
	} {
		if field.value != "" {
			target = append(target, field.label+": "+field.value)
		}
	}
	thread := m.status.ThreadID
	if m.snapshot != nil {
		if thread == "" {
			thread = m.snapshot.Metadata.ThreadID
		}
		// This is the thread's reported selection, not a guessed backend,
		// account, or assertion about which provider caused the error.
		if selection := m.snapshot.Metadata.ModelSelection; selection != nil {
			if selection.InstanceID != "" {
				target = append(target, "Thread provider: "+selection.InstanceID)
			}
			if selection.Model != "" {
				target = append(target, "Thread model: "+selection.Model)
			}
		}
	}
	if thread != "" {
		target = append(target, "Thread: "+thread)
	}
	if len(target) > 0 {
		parts = append(parts, strings.Join(target, "\n"))
	}
	parts = append(parts, "Next step: "+next)
	if m.problem != "" {
		parts = append(parts, "Error:\n"+m.problem)
	}
	if m.streamProblem != "" && m.streamProblem != m.problem {
		parts = append(parts, "Connection error:\n"+m.streamProblem)
	}
	return redactFeedback(strings.Join(parts, "\n\n"))
}

// openFeedbackDetail reports whether an error was opened. The parent router
// may call this for F2 or /error, including above an active request dialog.
// Closing the existing detail overlay restores that same dialog and query.
func (m *Model) openFeedbackDetail() bool {
	text := m.feedbackDetailText()
	if text == "" {
		return false
	}
	m.openDetail("Error details", text)
	return true
}

func (m *Model) feedbackDiagnosis() (diagnosis, next string) {
	text := strings.ToLower(m.problem + "\n" + m.streamProblem)
	switch {
	case m.inputProblem != "" && m.problem == m.inputProblem && strings.Contains(m.inputProblem, "not added"):
		return "Input kept · /recall", "Use /recall for the rejected text or /edit for the external editor. The previous draft was kept."
	case strings.Contains(text, "401"), strings.Contains(text, "unauthorized"),
		strings.Contains(text, "authentication"), strings.Contains(text, "expired token"),
		strings.Contains(text, "token expired"):
		return "Authentication needs attention", "/web shows this environment's configured T3 address. Reconnect the affected provider/account in T3, then /reconnect here."
	case strings.Contains(text, "403"), strings.Contains(text, "forbidden"):
		return "Access was denied", "Review access for this target in T3 (/web), then /reconnect here."
	case strings.Contains(text, "connection refused"), strings.Contains(text, "timeout"),
		strings.Contains(text, "timed out"), strings.Contains(text, "unreachable"),
		strings.Contains(text, "disconnected"), strings.Contains(text, "broken pipe"),
		strings.Contains(text, "no such host"):
		return "Connection needs attention", "Check this environment's connection, then /reconnect. Review /pending before retrying saved actions."
	default:
		diagnosis = m.problem
		if diagnosis == "" {
			diagnosis = m.streamProblem
		}
		diagnosis, _, _ = strings.Cut(redactFeedback(diagnosis), "\n")
		return diagnosis, "Review the error and target below. Correct the input or inspect T3 with /web; review /pending before retrying saved actions."
	}
}

func (m *Model) feedbackSummary(width int) string {
	diagnosis, _ := m.feedbackDiagnosis()
	hint := "Ctrl+L error"
	if m.preservedDraft != "" || m.dialog != nil && m.dialog.preservedInitial != "" {
		hint = "/error"
	}
	return ansi.Truncate(diagnosis, max(1, width-len(hint)-3), "…") + " · " + hint
}

var feedbackSecrets = []struct {
	pattern *regexp.Regexp
	replace string
}{
	{regexp.MustCompile(`(?i)(https?://)[^/\s@]+@`), "${1}[redacted]@"},
	{regexp.MustCompile(`(?i)(["']?\b(?:access[_-]?token|refresh[_-]?token|id[_-]?token|token|api[_-]?key|password|client[_-]?secret|secret|credential|authorization)["']?\s*[:=]\s*)(?:\[redacted\]|(?:bearer|basic)\s+(?:"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|(?:\\.|[^\s,;"'\\])+)|"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|[^\s&,;}\]]+)`), "${1}[redacted]"},
	{regexp.MustCompile(`(?i)\b(bearer|basic)\s+(?:"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|(?:\\.|[^\s,;"'\\])+)`), "${1} [redacted]"},
	{regexp.MustCompile(`(?i)([?&](?:code|auth|key)=)[^&#\s]+`), "${1}[redacted]"},
	{regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b`), "[redacted]"},
}

func redactFeedback(text string) string {
	text = safeText(text)
	for _, secret := range feedbackSecrets {
		text = secret.pattern.ReplaceAllString(text, secret.replace)
	}
	return text
}
