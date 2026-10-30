package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"
	"unicode"

	"github.com/Xon333/t3oxide/internal/client"
	"github.com/Xon333/t3oxide/internal/config"
	"github.com/Xon333/t3oxide/internal/ui"
	"github.com/Xon333/t3oxide/internal/wire"
	"github.com/charmbracelet/x/ansi"
)

var version = "0.9.0-bubbletea"

const help = `Usage: t3oxide [COMMAND] [OPTIONS]

Examples:
  t3oxide                                  open the TUI
  t3oxide --env m1 --thread THREAD_ID        open a specific conversation

In the TUI:
  Ctrl+O conversations · Ctrl+Space commands · /edit · /recall · /help
  Ctrl+P is a Commands fallback; /keyboard legacy retains editor Ctrl+P.
  Ctrl+S Stop + queue hold · Ctrl+C keeps draft · Ctrl+Q exits
  /retry [stop] reuses the saved operation ID; local exit leaves T3 running.

Read-only commands:
  t3oxide envs [--json]                     list configured environments
  t3oxide threads --env m1 [FILTER] [--json] list matching threads
  t3oxide status --env m1 [--thread ID]      adapter/thread status (always JSON)
  t3oxide doctor [--env m1] [--json]        verify identity and authenticated reads
  t3oxide capabilities [--env m1] [--json] describe client and optional host capabilities
  t3oxide activity --env m1 --thread ID    inspect the bounded activity page
  t3oxide inspect --env m1 --thread ID --item ID  read one tool/result

Options:
  --config PATH     existing environments.json (default ~/.config/t3oxide/)
  --state PATH      Go draft/recovery file (default ~/.config/t3oxide/go-drafts.json)
  --theme NAME      terminal, violet, cyan, or lime
  --no-mouse        native terminal selection; /mouse can change it later
  --json            machine-readable results for read commands; status always emits JSON
  --json-envelope   versioned threads JSON with completeness and continuation
  --scope NAME      threads: active, all, completed, failed
  --cursor N        threads: continuation offset from the previous envelope
  --limit N         threads: 1–200 results
  --type NAME       threads: all, main, subagents, forks
  --item ID         inspect: exact source item identity
  --revision KEY    inspect: observed revision (cache key, not a historical read)
  --activity-cursor KEY  activity: opaque earlier-page cursor
  --version         print version
  --help            print this help

Only human actions in the TUI send writes. These CLI commands are read-only.
Host credentials stay on the host. Existing T3 services are never started here.
`

type arguments struct {
	command, config, state, env, thread, theme              string
	json, jsonEnvelope, noMouse, version, help, listOptions bool
	scope                                                   string
	typeFilter, item, revision, activityCursor              string
	cursor, limit                                           int
	words                                                   []string
}

func parse(args []string) (arguments, error) {
	// Explicit help wins even when another argument is unknown, malformed,
	// or would otherwise consume the help flag as its missing value.
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			return arguments{help: true}, nil
		}
	}
	a := arguments{config: config.DefaultEnvironmentsPath(), state: config.DefaultStatePath()}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--help", "-h":
			a.help = true
		case "--version":
			a.version = true
		case "--json":
			a.json = true
		case "--json-envelope":
			a.json = true
			a.jsonEnvelope = true
		case "--scope", "--cursor", "--limit", "--type":
			a.listOptions = true
			if i+1 == len(args) {
				return a, fmt.Errorf("%s needs a value", arg)
			}
			i++
			value := args[i]
			if arg == "--type" {
				a.typeFilter = value
				if value != "all" && value != "main" && value != "subagents" && value != "forks" {
					return a, errors.New("type must be all, main, subagents or forks")
				}
			} else if arg == "--scope" {
				a.scope = value
				if value != "active" && value != "all" && value != "completed" && value != "failed" {
					return a, errors.New("scope must be active, all, completed or failed")
				}
			} else {
				n, err := strconv.Atoi(value)
				if err != nil || n < 0 || arg == "--limit" && (n < 1 || n > 200) || n > 1000000 {
					return a, errors.New("invalid list cursor/limit")
				}
				if arg == "--cursor" {
					a.cursor = n
				} else {
					a.limit = n
				}
			}
		case "--no-mouse":
			a.noMouse = true
		case "--config", "--state", "--env", "--thread", "--theme", "--item", "--revision", "--activity-cursor":
			if i+1 == len(args) {
				return a, fmt.Errorf("%s needs a value", arg)
			}
			i++
			value := args[i]
			switch arg {
			case "--config":
				a.config = value
			case "--state":
				a.state = value
			case "--env":
				a.env = value
			case "--thread":
				a.thread = value
			case "--theme":
				a.theme = value
			case "--item":
				a.item = value
			case "--revision":
				a.revision = value
			case "--activity-cursor":
				a.activityCursor = value
			}
		default:
			if strings.HasPrefix(arg, "-") {
				return a, fmt.Errorf("unknown option %s", arg)
			}
			if a.command == "" {
				a.command = arg
			} else {
				a.words = append(a.words, arg)
			}
		}
	}
	if a.command != "" && a.command != "envs" && a.command != "threads" && a.command != "status" && a.command != "doctor" && !inspectionCommand(a.command) {
		return a, fmt.Errorf("unknown command %q; use --help", a.command)
	}
	if (a.jsonEnvelope || a.listOptions) && a.command != "threads" {
		return a, errors.New("list format, scope and paging options require threads")
	}
	if (a.item != "" || a.revision != "") && a.command != "inspect" {
		return a, errors.New("item and revision options require inspect")
	}
	if a.activityCursor != "" && a.command != "activity" {
		return a, errors.New("activity-cursor requires activity")
	}
	if (a.command == "activity" || a.command == "inspect") && a.thread == "" {
		return a, errors.New("activity and inspect require --thread with the exact source thread ID")
	}
	if a.command == "inspect" && a.item == "" {
		return a, errors.New("inspect requires --item with the exact source item ID")
	}
	if a.command == "activity" || a.command == "inspect" {
		for _, id := range []string{a.thread, a.item, a.revision} {
			if len(id) > wire.MaxIdentifierBytes || strings.IndexFunc(id, unicode.IsControl) >= 0 {
				return a, errors.New("invalid inspection source identity or revision")
			}
		}
		if len(a.revision) > 128 {
			return a, errors.New("inspection revision exceeds 128 bytes")
		}
		if len(a.activityCursor) > 64*1024 {
			return a, errors.New("activity cursor exceeds 64 KiB")
		}
	}
	if a.theme != "" && !ui.ValidTheme(a.theme) {
		return a, errors.New("theme must be terminal, violet, cyan, or lime")
	}
	if len(a.words) > 0 && a.command != "threads" {
		return a, errors.New("unexpected positional arguments")
	}
	if a.json && a.command == "" && !a.version {
		return a, errors.New("--json requires a read-only command: envs, threads, status, doctor, capabilities, activity or inspect; use --help")
	}
	return a, nil
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "t3oxide:", plain(err.Error()))
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer) (err error) {
	a, err := parse(args)
	if inspectionCommand(a.command) && a.json {
		defer func() {
			if err != nil {
				err = errors.Join(err, writeInspectionError(out, a, err))
			}
		}()
	}
	if err != nil {
		return err
	}
	if a.help {
		_, err = io.WriteString(out, help)
		return err
	}
	if a.version {
		_, err = fmt.Fprintln(out, "t3oxide", version, "· Bubble Tea v2.1.0")
		return err
	}
	if a.command == "capabilities" && a.env == "" {
		return showCapabilities(out, a, nil)
	}
	envs, err := config.Load(a.config)
	if err != nil {
		return fmt.Errorf("%w; configure config/environments.example.json using your existing adapter paths (see NATIVE-GO.md)", err)
	}
	if len(envs) == 0 {
		return errors.New("no enabled environments; configure environments.json")
	}
	if a.command != "" {
		return headless(ctx, a, envs, out)
	}
	state, err := config.OpenState(a.state)
	if err != nil {
		return err
	}
	defer state.Close()
	// Copy only the old companion's draft/recovery format, once. The Swift file
	// stays untouched for rollback; no transcript or credential is copied.
	if a.state == config.DefaultStatePath() {
		if err = state.ImportLegacy(filepath.Join(config.DefaultDirectory(), "drafts.json")); err != nil {
			return err
		}
	}
	settings, err := ui.LoadSettings(filepath.Join(filepath.Dir(a.state), "terminal.json"))
	if err != nil {
		return err
	}
	if a.theme != "" {
		if err = settings.OverrideTheme(a.theme); err != nil {
			return err
		}
	}
	if a.noMouse {
		settings.Mouse = false
	}
	manager := client.NewManager(envs, state)
	manager.CreationProfile = a.env
	agent, err := manager.NewSession(a.env, a.thread)
	if err != nil {
		return err
	}
	err = ui.Run(ctx, agent, settings)
	return errors.Join(err, manager.Close())
}

func selected(a arguments, envs []config.Environment) (config.Environment, error) {
	for _, env := range envs {
		if env.ID == a.env || (a.env == "" && len(envs) == 1) {
			return env, nil
		}
	}
	return config.Environment{}, errors.New("choose --env PROFILE_ID (see t3oxide envs)")
}

func headless(ctx context.Context, a arguments, envs []config.Environment, out io.Writer) error {
	if inspectionCommand(a.command) {
		env, err := selected(a, envs)
		if err != nil {
			return err
		}
		return inspectCommand(ctx, a, env, out)
	}
	if a.command == "envs" {
		type row struct {
			ID            string `json:"id"`
			Label         string `json:"label"`
			Transport     string `json:"transport"`
			EnvironmentID string `json:"environmentId"`
		}
		rows := make([]row, 0, len(envs))
		for _, env := range envs {
			transport := "local"
			if env.SSHDestination != nil {
				transport = "ssh"
			}
			rows = append(rows, row{env.ID, env.Label, transport, env.EnvironmentID})
		}
		if a.json {
			return writeJSON(out, rows)
		}
		w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "PROFILE\tENVIRONMENT\tCONNECTION")
		for _, r := range rows {
			fmt.Fprintf(w, "%s\t%s\t%s\n", plain(r.ID), plain(r.Label), plain(r.Transport))
		}
		return w.Flush()
	}
	if a.command == "doctor" {
		if a.env != "" {
			env, err := selected(a, envs)
			if err != nil {
				return err
			}
			envs = []config.Environment{env}
		}
		var failures []error
		results := make([]map[string]any, 0, len(envs))
		for _, env := range envs {
			result, err := inspect(ctx, env, "", "")
			if err != nil {
				failures = append(failures, fmt.Errorf("%s: %w", env.ID, err))
				if a.json {
					results = append(results, map[string]any{"profile": env.ID, "ok": false, "error": err.Error()})
				} else {
					if _, writeErr := fmt.Fprintf(out, "%s: FAIL — %s\n", plain(env.ID), plain(err.Error())); writeErr != nil {
						return errors.Join(errors.Join(failures...), writeErr)
					}
				}
				continue
			}
			if a.json {
				results = append(results, map[string]any{"profile": env.ID, "ok": true, "status": result})
			} else {
				if _, writeErr := fmt.Fprintf(out, "%s: OK — identity verified, authenticated list, protocol 1 → T3 protocol 2\n", plain(env.ID)); writeErr != nil {
					return errors.Join(errors.Join(failures...), writeErr)
				}
			}
		}
		if a.json {
			if err := writeJSON(out, results); err != nil {
				failures = append(failures, err)
			}
		}
		return errors.Join(failures...)
	}
	env, err := selected(a, envs)
	if err != nil {
		return err
	}
	options := map[string]any{"lifecycle": a.scope, "cursor": a.cursor, "limit": a.limit}
	if a.typeFilter != "" {
		options["grouped"], options["typeFilter"] = true, a.typeFilter
	}
	result, err := inspectList(ctx, env, strings.Join(a.words, " "), a.thread, options)
	if err != nil {
		return err
	}
	if a.command == "status" || a.json {
		if a.command == "threads" {
			if a.jsonEnvelope {
				kind := a.typeFilter
				if kind == "" {
					kind = "all"
				}
				return writeJSON(out, map[string]any{"schemaVersion": 1, "profile": env.ID, "environmentId": env.EnvironmentID, "threads": result["threads"], "more": result["more"], "observedAt": result["observedAt"], "scope": result["lifecycle"], "type": kind, "cursor": result["cursor"], "nextCursor": result["nextCursor"]})
			}
			return writeJSON(out, result["threads"])
		}
		return writeJSON(out, result)
	}
	rows, ok := result["threads"].([]client.Thread)
	if !ok {
		return errors.New("invalid validated thread listing")
	}
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "THREAD\tTITLE\tPROJECT\tSTATE")
	for _, r := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", plain(r.ID), plain(r.Title), plain(r.Project), plain(r.Status))
	}
	if err = w.Flush(); err != nil {
		return err
	}
	if more, ok := result["more"].(bool); ok && more {
		_, err = fmt.Fprintln(out, "More matches exist; add a title/project filter.")
		return err
	}
	return nil
}

func inspect(parent context.Context, env config.Environment, query, thread string) (map[string]any, error) {
	return inspectList(parent, env, query, thread, nil)
}
func inspectList(parent context.Context, env config.Environment, query, thread string, options map[string]any) (map[string]any, error) {
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
	if _, requested := options["typeFilter"]; requested {
		features, _ := hello["features"].(map[string]any)
		if features["threadNavigation"] != true {
			return nil, &wire.Fault{Code: "unsupported", Message: "This adapter does not advertise thread type filtering."}
		}
	}
	fields := map[string]any{"query": query}
	for key, value := range options {
		if key == "lifecycle" && value == "" || key == "limit" && value == 0 {
			continue
		}
		fields[key] = value
	}
	listing, err := conn.Request(ctx, "list", fields, "")
	if err != nil {
		return nil, err
	}
	page, err := client.DecodeThreadPage(listing)
	threads, more := page.Threads, page.More
	if err != nil {
		return nil, err
	}
	result := map[string]any{"profile": env.ID, "adapter": hello, "threads": threads, "more": more, "observedAt": page.ObservedAt, "lifecycle": page.Lifecycle, "cursor": page.Cursor, "nextCursor": page.NextCursor}
	if thread == "" {
		return result, nil
	}
	var store wire.Store
	store.Select(thread)
	watch := make(chan error, 1)
	go func() { _, e := conn.Request(ctx, "watch", map[string]any{"threadId": thread}, ""); watch <- e }()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-conn.Done():
			return nil, conn.Err()
		case err := <-watch:
			watch = nil
			if err != nil {
				return nil, err
			}
		case packet, ok := <-conn.Events():
			if !ok {
				return nil, conn.Err()
			}
			if packet.Event != "view" && packet.Event != "delta" {
				continue
			}
			if _, err = store.Apply(packet.Event, packet.Data); err != nil {
				return nil, err
			}
			if snapshot := store.Snapshot(); snapshot != nil && snapshot.Metadata.Live {
				result["conversation"] = snapshot.Metadata
				result["visibleRows"] = len(snapshot.Items)
				result["revision"] = snapshot.Revision
				return result, nil
			}
		}
	}
}

func writeJSON(out io.Writer, value any) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func validateHello(result map[string]any, expected string) error {
	var hello struct {
		Protocol      int    `json:"protocol"`
		EnvironmentID string `json:"environmentId"`
		Orchestration int    `json:"orchestrationProtocol"`
	}
	encoded, err := json.Marshal(result)
	if err == nil {
		err = json.Unmarshal(encoded, &hello)
	}
	if err != nil || hello.Protocol != 1 || hello.EnvironmentID != expected || hello.Orchestration != 2 {
		return errors.New("adapter handshake identity or protocol mismatch")
	}
	return nil
}
func plain(text string) string {
	text = ansi.Strip(strings.ToValidUTF8(text, "�"))
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, text)
}
