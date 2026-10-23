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

var version = "0.4.0-bubbletea"

const help = `t3oxide — terminal companion for your existing T3 environments

  t3oxide                                  open the TUI
  t3oxide --env m1 --thread THREAD_ID        open a specific conversation
  t3oxide envs [--json]                     list configured environments
  t3oxide threads --env m1 [FILTER] [--json] list matching threads
  t3oxide status --env m1 [--thread ID]      read current adapter/thread status
  t3oxide doctor [--env m1]                 verify identity and authenticated reads

Options:
  --config PATH     existing environments.json (default ~/.config/t3oxide/)
  --state PATH      Go draft/recovery file (default ~/.config/t3oxide/go-drafts.json)
  --theme NAME      terminal, violet, cyan, or lime
  --no-mouse        native terminal selection; /mouse can change it later
  --json            machine-readable listing/status output
  --version         print version
  --help            print this help

TUI: /switch environments/threads · /threads FILTER · /search TEXT
     /delivery auto|queue|steer · Ctrl+S Stop + queue hold · /requests
     /earlier (pauses live detail) · /latest · /retry [stop] · /theme
     Enter sends · Ctrl+J newline · F3 commands · F6 conversations
     Ctrl+C keeps draft · Ctrl+X clears (F5 recalls) · Ctrl+Q exits
     F4 external editor · Tab transcript focus

Only human actions in the TUI send writes. These CLI commands are read-only.
Host credentials stay on the host. Existing T3 services are never started here.
`

type arguments struct {
	command, config, state, env, thread, theme string
	json, noMouse, version, help               bool
	words                                      []string
}

func parse(args []string) (arguments, error) {
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
		case "--no-mouse":
			a.noMouse = true
		case "--config", "--state", "--env", "--thread", "--theme":
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
	if a.command != "" && a.command != "envs" && a.command != "threads" && a.command != "status" && a.command != "doctor" {
		return a, fmt.Errorf("unknown command %q; use --help", a.command)
	}
	if a.theme != "" && !ui.ValidTheme(a.theme) {
		return a, errors.New("theme must be terminal, violet, cyan, or lime")
	}
	if len(a.words) > 0 && a.command != "threads" {
		return a, errors.New("unexpected positional arguments")
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

func run(ctx context.Context, args []string, out io.Writer) error {
	a, err := parse(args)
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
		settings.Theme = a.theme
	}
	if a.noMouse {
		settings.Mouse = false
	}
	manager := client.NewManager(envs, state)
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
	result, err := inspect(ctx, env, strings.Join(a.words, " "), a.thread)
	if err != nil {
		return err
	}
	if a.command == "status" || a.json {
		if a.command == "threads" {
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
	listing, err := conn.Request(ctx, "list", map[string]any{"query": query}, "")
	if err != nil {
		return nil, err
	}
	threads, more, err := client.DecodeThreads(listing)
	if err != nil {
		return nil, err
	}
	result := map[string]any{"profile": env.ID, "adapter": hello, "threads": threads, "more": more}
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
