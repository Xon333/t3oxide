package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/config"
	"github.com/Xon333/t3oxide/internal/wire"
)

// The new inspection commands share one versioned envelope. Existing array
// consumers of envs/threads and the existing status format remain unchanged.
type inspectionEnvelope struct {
	SchemaVersion int              `json:"schemaVersion"`
	OK            bool             `json:"ok"`
	Command       string           `json:"command"`
	Profile       string           `json:"profile,omitempty"`
	EnvironmentID string           `json:"environmentId,omitempty"`
	Data          any              `json:"data,omitempty"`
	Error         *inspectionError `json:"error,omitempty"`
}

type inspectionError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Uncertain bool   `json:"uncertain"`
}

func inspectionCommand(command string) bool {
	return command == "capabilities" || command == "activity" || command == "inspect"
}

func writeInspectionError(out io.Writer, a arguments, err error) error {
	code := "read_failed"
	var fault *wire.Fault
	if errors.As(err, &fault) {
		code = fault.Code
	} else if errors.Is(err, context.Canceled) {
		code = "cancelled"
	} else if errors.Is(err, context.DeadlineExceeded) {
		code = "timeout"
	}
	// These commands cannot dispatch a mutation. A failed read is not an
	// uncertain write and never creates a local operation record.
	return writeJSON(out, inspectionEnvelope{SchemaVersion: 1, Command: a.command,
		Profile: a.env, Error: &inspectionError{Code: code, Message: plain(err.Error())}})
}

func clientCapabilities() map[string]any {
	commands := []map[string]any{
		{"name": "envs", "readOnly": true, "scope": "configured profiles", "json": "array"},
		{"name": "threads", "readOnly": true, "scope": "bounded shell metadata", "json": "array or --json-envelope", "filters": []string{"--scope", "--type", "--cursor", "--limit"}},
		{"name": "status", "readOnly": true, "scope": "profile or one thread", "json": "object"},
		{"name": "doctor", "readOnly": true, "scope": "identity and authenticated reads", "json": "array"},
		{"name": "capabilities", "readOnly": true, "scope": "client; optional --env handshake", "json": "envelope-v1"},
		{"name": "activity", "readOnly": true, "scope": "bounded tool page", "json": "envelope-v1", "required": []string{"--thread"}, "paging": "--activity-cursor"},
		{"name": "inspect", "readOnly": true, "scope": "one exact source item", "json": "envelope-v1", "required": []string{"--thread", "--item"}, "optional": []string{"--revision"}},
	}
	tui := make([]map[string]string, 0)
	for _, c := range client.CommandCatalog() {
		tui = append(tui, map[string]string{"name": c.Name, "description": c.Description})
	}
	return map[string]any{
		"product": "t3oxide", "version": version,
		"commands": commands, "sessionActions": tui,
		"protocol":          map[string]any{"companion": 1, "orchestration": 2},
		"transport":         map[string]any{"local": "owned stdio helper", "remote": "existing OpenSSH route to owned stdio helper", "credentials": "host only", "backgroundService": false},
		"bounds":            map[string]any{"retainedDetailedAttachments": 1, "temporarySourceReviews": 1, "defaultRootConversations": 24, "defaultChildLoading": false, "maxFrameBytes": wire.MaxFrameBytes, "maxReadableRows": wire.MaxRows, "maxReadableBytes": wire.MaxRowsBytes, "maxActivityItems": wire.MaxActivityItems, "maxActivityBytes": wire.MaxActivityBytes, "maxActivityDetailBytes": wire.MaxActivityDetailBytes},
		"mutationAccess":    "interactive-only",
		"recovery":          map[string]any{"durableIdentity": true, "automaticMutationReplay": false, "stop": "reviewed app run with queue hold"},
		"revisionSemantics": "getTurnItem revision is a cache key; returned details can differ",
	}
}

func showCapabilities(out io.Writer, a arguments, host map[string]any) error {
	data := clientCapabilities()
	if host != nil {
		// Publish negotiated capabilities, never arbitrary future handshake fields.
		advertised := map[string]any{}
		for _, name := range []string{"protocol", "environmentId", "orchestrationProtocol", "serverVersion", "adapterRevision", "writesEnabled"} {
			if value, exists := host[name]; exists {
				advertised[name] = value
			}
		}
		features := map[string]bool{}
		if raw, ok := host["features"].(map[string]any); ok {
			for name, value := range raw {
				if flag, ok := value.(bool); ok {
					features[name] = flag
				}
			}
		}
		advertised["features"] = features
		data["adapter"] = advertised
	}
	if a.json {
		return writeJSON(out, inspectionEnvelope{SchemaVersion: 1, OK: true, Command: "capabilities", Profile: a.env, Data: data})
	}
	if _, err := fmt.Fprintf(out, "t3oxide %s\n\n", version); err != nil {
		return err
	}
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "COMMAND\tSCOPE\tJSON")
	for _, row := range data["commands"].([]map[string]any) {
		fmt.Fprintf(w, "%s\t%s\t%s\n", row["name"], row["scope"], row["json"])
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if host != nil {
		_, err := fmt.Fprintf(out, "\n%s · adapter handshake verified · T3 %s\n", plain(a.env), plain(fmt.Sprint(host["serverVersion"])))
		return err
	}
	return nil
}

// oneRead owns one helper and always reaps it. It does not open local state,
// watch a transcript, renew credentials, start T3, or request write authority.
func oneRead(parent context.Context, env config.Environment, operation string, fields map[string]any) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(parent, 25*time.Second)
	defer cancel()
	conn, err := wire.Start(ctx, env)
	if err != nil {
		return nil, err
	}
	defer func() {
		conn.Close()
		closeCtx, done := context.WithTimeout(context.Background(), 3*time.Second)
		defer done()
		_ = conn.WaitClosed(closeCtx)
	}()
	hello, err := conn.Request(ctx, "hello", nil, "")
	if err != nil {
		return nil, err
	}
	if err = validateHello(hello, env.EnvironmentID); err != nil {
		return nil, err
	}
	if operation == "hello" {
		return hello, nil
	}
	features, _ := hello["features"].(map[string]any)
	feature := "activityInspection"
	if operation == "activity" {
		feature = "activity"
	}
	if features[feature] != true {
		return nil, &wire.Fault{Code: "unsupported", Message: "This adapter does not advertise activity inspection. Update this host adapter to use it."}
	}
	return conn.Request(ctx, operation, fields, "")
}

func inspectCommand(ctx context.Context, a arguments, env config.Environment, out io.Writer) error {
	if a.command == "capabilities" {
		hello, err := oneRead(ctx, env, "hello", nil)
		if err != nil {
			return err
		}
		return showCapabilities(out, a, hello)
	}
	operation, fields := "activity", map[string]any{"threadId": a.thread}
	if a.activityCursor != "" {
		fields["cursor"] = a.activityCursor
	}
	if a.command == "inspect" {
		operation, fields["itemId"] = "inspect-item", a.item
		if a.revision != "" {
			fields["revision"] = a.revision
		}
	}
	result, err := oneRead(ctx, env, operation, fields)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	var data any
	if a.command == "activity" {
		page, err := wire.DecodeActivityList(raw)
		if err != nil {
			return &wire.Fault{Code: "invalid_response", Message: err.Error()}
		}
		if page.ThreadID != a.thread {
			return &wire.Fault{Code: "identity_mismatch", Message: "Activity page returned a different source thread."}
		}
		data = page
	} else {
		detail, err := wire.DecodeActivityDetail(raw)
		if err != nil {
			return &wire.Fault{Code: "invalid_response", Message: err.Error()}
		}
		if detail.SourceThreadID != a.thread || detail.ItemID != a.item || detail.RequestedRevision != a.revision {
			return &wire.Fault{Code: "identity_mismatch", Message: "Item inspection returned a different source or revision request."}
		}
		data = detail
	}
	if a.json {
		return writeJSON(out, inspectionEnvelope{SchemaVersion: 1, OK: true, Command: a.command, Profile: env.ID, EnvironmentID: env.EnvironmentID, Data: data})
	}
	if page, ok := data.(wire.ActivityList); ok {
		return writeActivityList(out, env.Label, page)
	}
	return writeActivityDetail(out, env.Label, data.(wire.ActivityDetail))
}

func writeActivityList(out io.Writer, environment string, page wire.ActivityList) error {
	title := page.Title
	if title == "" {
		title = page.ThreadID
	}
	if _, err := fmt.Fprintf(out, "%s / %s\n", plain(environment), plain(title)); err != nil {
		return err
	}
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "SOURCE THREAD\tITEM\tACTIVITY\tSTATE")
	for _, row := range page.Activity.Items {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", plain(row.SourceThreadID), plain(row.ItemID), plain(row.Title), plain(row.Status))
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if page.Activity.HasMore {
		_, err := fmt.Fprintln(out, "More activity exists. Use --json for the continuation cursor.")
		return err
	}
	if page.Activity.Limited {
		_, err := fmt.Fprintln(out, "One or more activity summaries were shortened. Inspect the item for details.")
		return err
	}
	return nil
}

func writeActivityDetail(out io.Writer, environment string, detail wire.ActivityDetail) error {
	var content strings.Builder
	fmt.Fprintf(&content, "%s / %s / %s\n%s · %s\n", plain(environment), plain(detail.SourceThreadID), plain(detail.ItemID), plain(detail.Title), plain(detail.Status))
	if detail.Changed {
		content.WriteString("Result revision differs from the requested revision.\n")
	} else if detail.RequestedRevision != "" && detail.UpdatedAt == "" {
		content.WriteString("Result revision unreported.\n")
	}
	if detail.Input != "" {
		content.WriteString("\nInput\n" + terminalText(detail.Input) + "\n")
	}
	switch {
	case detail.OutputPresent:
		if detail.Output == "" {
			content.WriteString("\nReturned output is empty.\n")
		} else {
			content.WriteString("\nOutput\n" + terminalText(detail.Output) + "\n")
		}
	case detail.OutputOmitted:
		content.WriteString("\nOutput is not available from this response.\n")
	default:
		content.WriteString("\nNo result reported.\n")
	}
	if detail.OutputPresent && detail.OutputOmitted {
		content.WriteString("\nT3 reports additional omitted output.\n")
	}
	if detail.Truncated {
		content.WriteString("\nResult shortened to the inspection limit.\n")
	}
	_, err := io.WriteString(out, content.String())
	return err
}

// Preserve result line boundaries while excluding terminal control sequences.
func terminalText(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i, line := range lines {
		lines[i] = plain(line)
	}
	return strings.Join(lines, "\n")
}
