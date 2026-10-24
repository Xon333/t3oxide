package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

func (a *Session) Commands() []CommandSpec { return CommandCatalog() }

// CommandCatalog is also available before a session is selected. Consumers
// reuse these supported names instead of maintaining a disconnected catalog.
func CommandCatalog() []CommandSpec {
	return []CommandSpec{
		{Name: "activity", Description: "Inspect loaded tools and results; explicit earlier activity"},
		{Name: "model", Description: "Choose the current thread model; optional next stages one message"},
		{Name: "settings", Description: "Stage the current model and advertised options, then apply once"},
		{Name: "effort", Description: "Choose supported reasoning effort; optional next"},
		{Name: "speed", Description: "Choose advertised service tier / fast mode; optional next"},
		{Name: "presets", Description: "Apply, save or remove environment-scoped model favorites"},
		{Name: "override", Description: "Stage or clear one message model/effort/speed override"},
		{Name: "runtime", Description: "Compare saved, run-requested and provider-reported selection"},
		{Name: "parent", Description: "Jump to the authoritative parent of this subagent/fork"},
		{Name: "filter", Description: "Browse All / Main / Subagents / Forks"},
		{Name: "switch", Description: "Switch environment or conversation"},
		{Name: "env", Description: "Choose an environment"},
		{Name: "threads", Description: "Choose conversation; optional title/project filter"},
		{Name: "earlier", Description: "Replace view with earlier history; pause live detail"},
		{Name: "latest", Description: "Return to the latest live conversation"},
		{Name: "delivery", Description: "Set T3 delivery: auto, queue, or steer"},
		{Name: "stop", Description: "Stop reviewed active turn with queue hold (Ctrl+S)"},
		{Name: "requests", Description: "Review exact pending T3 approvals and questions"},
		{Name: "new", Description: "New conversation using this existing workspace"},
		{Name: "pending", Description: "Inspect saved operation IDs and original intent"},
		{Name: "retry", Description: "Explicit same-ID retry; /retry stop for Stop lane"},
		{Name: "resolve", Description: "Review and clear a local retry record; optional stop"},
		{Name: "reconnect", Description: "Reconnect owned helper; never replays writes"},
		{Name: "web", Description: "Show this environment's full T3 URL"},
	}
}

// Command runs outside the UI event loop. Local cancellation detaches only;
// the explicit stop command is the sole way this client stops remote work.
func (a *Session) Command(ctx context.Context, name, args string) (result Result, err error) {
	if ctx == nil {
		return result, errors.New("command requires an active context")
	}
	commandCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	a.mu.Lock()
	session := a.ctx
	a.mu.Unlock()
	if session != nil {
		stop := context.AfterFunc(session, cancel)
		defer stop()
	}
	result, err = a.invoke(commandCtx, strings.TrimPrefix(name, "/"), strings.TrimSpace(args))
	a.rememberError(err)
	return result, err
}

func (a *Session) invoke(ctx context.Context, name, args string) (Result, error) {
	var result Result
	switch name {
	case "activity":
		return a.activity(ctx, args)
	case "settings":
		return a.settings(ctx)
	case "model":
		return a.pickModel(ctx, args)
	case "effort", "speed":
		return a.pickOption(ctx, name, args)
	case "presets":
		return a.presets(ctx, args)
	case "override":
		return a.override(ctx, args)
	case "runtime":
		return a.runtimeResult()
	case "parent":
		next, err := a.Parent(ctx)
		return Result{Next: next}, err
	case "filter":
		next, err := a.filterTarget(ctx, args)
		return Result{Next: next}, err
	case "switch":
		return a.Browse(ctx, args)
	case "env":
		a.mu.Lock()
		profile := ""
		if a.environment != nil {
			profile = a.environment.ID
		}
		a.mu.Unlock()
		scope, err := a.chooseScope(ctx, a.manager.browserScope(profile))
		if err != nil {
			return result, err
		}
		if len(scope) == 0 {
			return result, nil
		}
		return a.Browse(ctx, args)
	case "threads":
		return a.Browse(ctx, args)
	case "earlier", "latest":
		return result, a.Navigate(ctx, name == "earlier")
	case "delivery":
		if err := a.SetDelivery(args); err != nil {
			return result, err
		}
		result.Text = "T3 delivery: " + args
		return result, nil
	case "stop":
		return result, a.Stop(ctx)
	case "requests":
		return result, a.ReviewRequests(ctx)
	case "retry":
		if args != "" && args != "stop" {
			return result, errors.New("use /retry or /retry stop")
		}
		reply, err := a.Retry(ctx, args == "stop")
		if err != nil {
			return result, err
		}
		result.Text = "Original operation acknowledged by T3."
		if thread := stringValue(reply["threadId"]); thread != "" {
			a.mu.Lock()
			env := a.environment
			a.mu.Unlock()
			target, err := a.Target(env.ID, thread)
			result.Next = target
			return result, err
		}
		return result, nil
	case "resolve":
		if args != "" && args != "stop" {
			return result, errors.New("use /resolve or /resolve stop")
		}
		return result, a.Resolve(ctx, args == "stop")
	case "pending":
		a.mu.Lock()
		env := a.environment
		a.mu.Unlock()
		if env == nil {
			return result, errors.New("select an environment")
		}
		records := map[string]any{"action": a.manager.State.Pending(env.EnvironmentID, false), "stop": a.manager.State.Pending(env.EnvironmentID, true)}
		encoded, _ := json.MarshalIndent(records, "", "  ")
		result.DetailTitle = "Saved operations · explicit retry only"
		result.Detail = "```json\n" + string(encoded) + "\n```\n\n`/retry` / `/retry stop` reuse the exact operation ID. `/resolve` clears a local record only after review."
		return result, nil
	case "reconnect":
		a.mu.Lock()
		a.connectionState = "reconnecting"
		a.ready, a.writable = false, false
		a.mu.Unlock()
		a.signal()
		select {
		case a.reconnect <- struct{}{}:
		default:
		}
		return result, nil
	case "web":
		a.mu.Lock()
		env := a.environment
		a.mu.Unlock()
		if env == nil || env.WebURL == nil {
			return result, errors.New("no webURL is configured for this environment")
		}
		result.DetailTitle, result.Detail = "Full T3", *env.WebURL
		return result, nil
	case "new":
		return a.newConversation(ctx, args)
	default:
		return result, fmt.Errorf("unsupported command /%s", name)
	}
}

func launchDraftThread(thread string) string { return "\x00new/" + thread }

func (a *Session) switchTarget(ctx context.Context, query string) (*Session, error) {
	a.mu.Lock()
	env := a.environment
	a.mu.Unlock()
	if env == nil {
		chosen, err := a.chooseEnvironment(ctx)
		if err != nil {
			return nil, err
		}
		return a.Target(chosen.ID, "")
	}
	return a.filteredTarget(ctx, query)
}
