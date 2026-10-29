package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Xon333/t3oxide/internal/config"
	"github.com/Xon333/t3oxide/internal/wire"
)

type NativeProject struct{ ID, Title, WorkspaceRoot string }
type nativeLaunchDefaults struct {
	ProjectID         string                   `json:"projectId"`
	ProjectTitle      string                   `json:"projectTitle"`
	WorkspaceRoot     string                   `json:"workspaceRoot"`
	ModelSelection    config.ModelSelection    `json:"modelSelection"`
	RuntimeMode       string                   `json:"runtimeMode"`
	InteractionMode   string                   `json:"interactionMode"`
	WorkspaceMode     string                   `json:"workspaceMode"`
	WorkspaceStrategy config.WorkspaceStrategy `json:"workspaceStrategy"`
	Revision          string                   `json:"revision"`
	Catalog           ModelCatalog             `json:"catalog"`
}

func (a *Session) creationEnvironment(ctx context.Context) (config.Environment, error) {
	profile := a.manager.CreationProfile
	if profile == "" {
		for _, env := range a.manager.Environments {
			if env.DefaultForNew {
				if profile != "" {
					return config.Environment{}, errors.New("multiple creation defaults")
				}
				profile = env.ID
			}
		}
	}
	if profile == "" && len(a.manager.Environments) == 1 {
		return a.manager.Environments[0], nil
	}
	if profile != "" {
		return a.navigatorEnvironment(profile)
	}
	choices := make([]Choice, 0, len(a.manager.Environments))
	for _, env := range a.manager.Environments {
		choices = append(choices, Choice{ID: env.ID, Label: env.Label, Description: env.EnvironmentID})
	}
	if len(choices) == 0 {
		return config.Environment{}, errors.New("no enabled environments")
	}
	answer, err := a.asker.Ask(ctx, Question{Kind: ChooseOne, Title: "New in environment", Prompt: "T3's desktop default environment is not exposed by this host API. Choose the target, or set defaultForNew in the profile file.", Choices: choices})
	if err != nil {
		return config.Environment{}, err
	}
	if answer.Cancelled {
		return config.Environment{}, context.Canceled
	}
	if len(answer.ChoiceIDs) != 1 {
		return config.Environment{}, errors.New("choose one offered environment")
	}
	return a.navigatorEnvironment(answer.ChoiceIDs[0])
}
func nativeConnection(ctx context.Context, env config.Environment) (*wire.Connection, error) {
	conn, err := wire.Start(ctx, env)
	if err != nil {
		return nil, err
	}
	hello, err := conn.Request(ctx, "hello", nil, "")
	if err == nil && (stringValue(hello["environmentId"]) != env.EnvironmentID || numberValue(hello["protocol"]) != 1 || numberValue(hello["orchestrationProtocol"]) != 2) {
		err = errors.New("native workflow helper identity or protocol mismatch")
	}
	features, _ := hello["features"].(map[string]any)
	if err == nil && (!boolValue(features["nativeWorkflows"]) || !boolValue(hello["writesEnabled"])) {
		err = errors.New("this host does not expose writable native project workflows; adopt its compatible adapter")
	}
	if err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}
func closeNativeConnection(conn *wire.Connection) {
	conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = conn.WaitClosed(ctx)
}
func decodeProjects(result map[string]any) ([]NativeProject, *int, error) {
	b, err := json.Marshal(result["projects"])
	var projects []NativeProject
	if err != nil || len(b) == 0 || b[0] != '[' || len(b) > 256*1024 || json.Unmarshal(b, &projects) != nil || len(projects) > 200 {
		return nil, nil, errors.New("invalid bounded native project list")
	}
	seen := map[string]bool{}
	for _, p := range projects {
		if p.ID == "" || len(p.ID) > 4096 || strings.ContainsAny(p.ID, "\x00\n\r") || seen[p.ID] || len(p.Title) > 512 || !strings.HasPrefix(p.WorkspaceRoot, "/") || len(p.WorkspaceRoot) > 4096 || strings.ContainsAny(p.WorkspaceRoot, "\x00\n\r") {
			return nil, nil, errors.New("invalid native project identity")
		}
		seen[p.ID] = true
	}
	var next *int
	if value := result["nextCursor"]; value != nil {
		n := numberValue(value)
		if n <= 0 || n > 1000000 {
			return nil, nil, errors.New("invalid native project continuation")
		}
		v := int(n)
		next = &v
	}
	return projects, next, nil
}
func (a *Session) chooseNativeProject(ctx context.Context, conn *wire.Connection, wanted string) (NativeProject, error) {
	cursor := 0
	missingSource := false
	for {
		result, err := conn.Request(ctx, "projects", map[string]any{"cursor": cursor, "limit": 200}, "")
		if err != nil {
			return NativeProject{}, err
		}
		projects, next, err := decodeProjects(result)
		if err != nil {
			return NativeProject{}, err
		}
		if next != nil && (*next <= cursor || len(projects) == 0) {
			return NativeProject{}, errors.New("native project continuation did not advance")
		}
		for _, p := range projects {
			if wanted != "" && p.ID == wanted {
				return p, nil
			}
		}
		// Search later native pages for the same exact project identity before
		// requiring a choice; a title match never retargets across environments.
		if wanted != "" && next != nil {
			cursor = *next
			continue
		}
		if wanted != "" && cursor != 0 {
			missingSource = true
			wanted = ""
			cursor = 0
			continue
		}
		prompt := ""
		if wanted != "" || missingSource {
			prompt = "The source project is not registered in this environment. Choose an existing project."
		}
		choices := make([]Choice, 0, len(projects)+1)
		for _, p := range projects {
			choices = append(choices, Choice{ID: p.ID, Label: p.Title, Description: p.WorkspaceRoot})
		}
		if next != nil {
			choices = append(choices, Choice{ID: "\x00more", Label: "More projects"})
		}
		if cursor > 0 {
			choices = append(choices, Choice{ID: "\x00first", Label: "First page"})
		}
		if len(choices) == 0 {
			return NativeProject{}, errors.New("no existing project; create the project in T3")
		}
		answer, err := a.asker.Ask(ctx, Question{Kind: ChooseOne, Title: "Project", Prompt: prompt, Choices: choices})
		if err != nil {
			return NativeProject{}, err
		}
		if answer.Cancelled {
			return NativeProject{}, context.Canceled
		}
		if len(answer.ChoiceIDs) != 1 {
			return NativeProject{}, errors.New("choose one offered project")
		}
		id := answer.ChoiceIDs[0]
		if id == "\x00more" && next != nil {
			cursor = *next
			wanted = ""
			continue
		}
		if id == "\x00first" && cursor > 0 {
			cursor = 0
			wanted = ""
			continue
		}
		for _, p := range projects {
			if p.ID == id {
				return p, nil
			}
		}
		return NativeProject{}, errors.New("project was not offered")
	}
}
func readNativeDefaults(ctx context.Context, conn *wire.Connection, project string, isolated bool, workspace string) (nativeLaunchDefaults, error) {
	fields := map[string]any{"projectId": project, "isolated": isolated}
	if workspace != "" {
		fields["workspaceMode"] = workspace
	}
	result, err := conn.Request(ctx, "launch-defaults", fields, "")
	if err != nil {
		return nativeLaunchDefaults{}, err
	}
	var d nativeLaunchDefaults
	b, err := json.Marshal(result)
	if err != nil || json.Unmarshal(b, &d) != nil || d.ProjectID != project || len(d.Revision) != 64 || strings.Trim(d.Revision, "0123456789abcdef") != "" || !strings.HasPrefix(d.WorkspaceRoot, "/") || len(d.WorkspaceRoot) > 4096 || strings.ContainsAny(d.WorkspaceRoot, "\x00\n\r") {
		return d, errors.New("invalid native launch defaults")
	}
	if err = d.ModelSelection.Validate(); err != nil {
		return d, err
	}
	if err = d.WorkspaceStrategy.Validate(); err != nil {
		return d, err
	}
	if d.WorkspaceMode != "local" && d.WorkspaceMode != "worktree" || d.WorkspaceMode == "local" && d.WorkspaceStrategy.Type != "root" || d.WorkspaceMode == "worktree" && d.WorkspaceStrategy.Type != "worktree" {
		return d, errors.New("native workspace mode and strategy differ")
	}
	if d.InteractionMode != "default" && d.InteractionMode != "plan" {
		return d, errors.New("invalid native interaction default")
	}
	switch d.RuntimeMode {
	case "approval-required", "auto-accept-edits", "auto", "full-access":
	default:
		return d, errors.New("invalid native runtime default")
	}
	catalogMap, ok := result["catalog"].(map[string]any)
	if !ok {
		return d, errors.New("native launch catalog missing")
	}
	catalog, err := DecodeCatalog(catalogMap)
	if err != nil {
		return d, err
	}
	if catalog.ThreadID != project || !catalog.Current.Equal(d.ModelSelection) {
		return d, errors.New("native launch catalog identity mismatch")
	}
	d.Catalog = *catalog
	return d, nil
}

// NewConversation never inherits settings or workspace from the active thread.
// Same-project means its exact project identity is the only carried value.
func (a *Session) NewConversation(ctx context.Context, title string, sameProject, isolated bool) (Result, error) {
	project := ""
	if sameProject {
		s := a.snapshot()
		if s == nil || s.Metadata.ProjectID == nil {
			return Result{}, errors.New("select a conversation with a native project first")
		}
		project = *s.Metadata.ProjectID
	}
	return a.NewConversationForProject(ctx, title, project, isolated)
}
func (a *Session) FileWork(ctx context.Context, title string) (Result, error) {
	return a.NewConversation(ctx, title, false, true)
}
func (a *Session) NewConversationForProject(ctx context.Context, title, projectID string, isolated bool) (Result, error) {
	return a.newConversationInEnvironment(ctx, title, projectID, isolated, "", nil)
}
func (a *Session) newConversationInEnvironment(ctx context.Context, title, projectID string, isolated bool, explicitProfile string, seed *LaunchInput) (Result, error) {
	if ctx == nil {
		return Result{}, errors.New("new conversation requires an active context")
	}
	if err := a.flushDraft(); err != nil {
		return Result{}, err
	}
	var env config.Environment
	var err error
	if explicitProfile != "" {
		env, err = a.navigatorEnvironment(explicitProfile)
	} else {
		env, err = a.creationEnvironment(ctx)
	}
	if errors.Is(err, context.Canceled) && ctx.Err() == nil {
		return Result{}, nil
	}
	if err != nil {
		return Result{}, err
	}
	conn, err := nativeConnection(ctx, env)
	if err != nil {
		return Result{}, err
	}
	defer closeNativeConnection(conn)
	project, err := a.chooseNativeProject(ctx, conn, projectID)
	if errors.Is(err, context.Canceled) && ctx.Err() == nil {
		return Result{}, nil
	}
	if err != nil {
		return Result{}, err
	}
	d, err := readNativeDefaults(ctx, conn, project.ID, isolated, "")
	if err != nil {
		return Result{}, err
	}
	formSource := "project-launch/" + project.ID
	formSource = batchLaunchFormSource(ctx, formSource)
	input := LaunchInput{Title: title}
	if input.Title == "" {
		input.Title = "New conversation"
	}
	selected := d.ModelSelection.Clone()
	workspaceMode := d.WorkspaceMode
	saved, hasSaved := a.manager.State.LaunchDraft(env.EnvironmentID, formSource)
	if hasSaved {
		input.Message = saved.Message
		if title == "" {
			input.Title = saved.Title
		}
		if saved.Selection != nil {
			selected = saved.Selection.Clone()
		}
		if saved.WorkspaceStrategy != nil {
			workspaceMode = "local"
			if saved.WorkspaceStrategy.Type == "worktree" {
				workspaceMode = "worktree"
			}
			d, err = readNativeDefaults(ctx, conn, project.ID, isolated, workspaceMode)
			if err != nil {
				return Result{}, err
			}
		}
	}
	if seed != nil && (!hasSaved || batchLaunchContext(ctx) == nil) {
		input = *seed
	}
	stale := hasSaved && saved.NativeDefaultsRevision != "" && saved.NativeDefaultsRevision != d.Revision
	if stale {
		// Restoring a form does not accept a changed workspace, permission mode,
		// or provider default. Retain its exact reviewed binding until Defaults.
		d.Revision = saved.NativeDefaultsRevision
		if saved.NativeWorkspaceRoot != "" {
			d.WorkspaceRoot = saved.NativeWorkspaceRoot
		}
		if saved.WorkspaceStrategy != nil {
			d.WorkspaceStrategy = *saved.WorkspaceStrategy
		}
		if saved.RuntimeMode != "" {
			d.RuntimeMode = saved.RuntimeMode
		}
		if saved.InteractionMode != "" {
			d.InteractionMode = saved.InteractionMode
		}
	}
	for {
		location := Location{ProfileID: env.ID, EnvironmentID: env.EnvironmentID, Environment: env.Label, ProjectID: project.ID, Title: project.Title, Workspace: d.WorkspaceRoot}
		if d.WorkspaceStrategy.Type == "worktree" {
			location.Workspace = "New worktree from " + d.WorkspaceStrategy.BaseRef + " · " + d.WorkspaceRoot
		}
		actions := launchActions(&d.Catalog, &selected, true)
		actions = append(actions, "workspace", "defaults", "project", "environment")
		bound := d
		boundSelection := selected.Clone()
		store := func(in LaunchInput) error {
			strategy := bound.WorkspaceStrategy
			return a.manager.State.SetLaunchDraft(env.EnvironmentID, formSource, config.LaunchDraft{Title: in.Title, Message: in.Message, ProjectID: project.ID, Workspace: location.Workspace, Selection: &boundSelection, NativeDefaultsRevision: bound.Revision, NativeWorkspaceRoot: bound.WorkspaceRoot, WorkspaceStrategy: &strategy, RuntimeMode: bound.RuntimeMode, InteractionMode: bound.InteractionMode})
		}
		unavailable := ""
		if stale {
			unavailable = "Native defaults changed. Choose Defaults to review their current values before Create."
		}
		review := &LaunchReview{Input: input, Location: location, Selection: &selected, Summary: selected.Summary() + " · " + d.RuntimeMode + " · " + d.InteractionMode, Actions: actions, Unavailable: unavailable, StoreDraft: store}
		answer, err := a.asker.Ask(ctx, Question{Surface: SurfaceLaunch, Kind: EnterText, Title: "New conversation", LaunchReview: review})
		if err != nil {
			return Result{}, err
		}
		if answer.Launch != nil {
			input = *answer.Launch
		}
		if err = store(input); err != nil {
			return Result{}, err
		}
		if answer.Cancelled || answer.Action == "cancel" {
			return Result{}, nil
		}
		switch answer.Action {
		case "project":
			closeNativeConnection(conn)
			return a.newConversationInEnvironment(ctx, input.Title, "", isolated, env.ID, &input)
		case "environment":
			choices := []Choice{}
			for _, target := range a.manager.Environments {
				choices = append(choices, Choice{ID: target.ID, Label: target.Label, Description: target.EnvironmentID})
			}
			choice, err := a.asker.Ask(ctx, Question{Kind: ChooseOne, Title: "New in environment", Initial: env.ID, Choices: choices})
			if err != nil {
				return Result{}, err
			}
			if choice.Cancelled {
				continue
			}
			if len(choice.ChoiceIDs) != 1 {
				return Result{}, errors.New("choose one offered environment")
			}
			target, err := a.navigatorEnvironment(choice.ChoiceIDs[0])
			if err != nil {
				return Result{}, err
			}
			closeNativeConnection(conn)
			return a.newConversationInEnvironment(ctx, input.Title, "", isolated, target.ID, &input)
		case "workspace":
			answer, err := a.asker.Ask(ctx, Question{Kind: ChooseOne, Title: "Workspace", Initial: workspaceMode, Choices: []Choice{{ID: "worktree", Label: "New isolated worktree", Description: "T3 creates and binds a fresh checkout"}, {ID: "local", Label: "Shared project root", Description: "Explicitly use the existing project checkout"}}})
			if err != nil {
				return Result{}, err
			}
			if answer.Cancelled {
				continue
			}
			if len(answer.ChoiceIDs) != 1 || (answer.ChoiceIDs[0] != "local" && answer.ChoiceIDs[0] != "worktree") {
				return Result{}, errors.New("workspace was not offered")
			}
			workspaceMode = answer.ChoiceIDs[0]
			fresh, readErr := readNativeDefaults(ctx, conn, project.ID, isolated, workspaceMode)
			err = readErr
			if err != nil {
				return Result{}, err
			}
			if stale {
				d.WorkspaceMode, d.WorkspaceStrategy, d.WorkspaceRoot, d.Catalog = fresh.WorkspaceMode, fresh.WorkspaceStrategy, fresh.WorkspaceRoot, fresh.Catalog
			} else {
				d = fresh
			}
		case "defaults":
			d, err = readNativeDefaults(ctx, conn, project.ID, isolated, workspaceMode)
			if err != nil {
				return Result{}, err
			}
			selected = d.ModelSelection.Clone()
			stale = false
		case "model", "effort", "tier":
			selected, err = a.pickLaunchSelectionWithDefaults(ctx, &d.Catalog, selected, answer.Action, true)
			if err != nil {
				return Result{}, err
			}
		case "create":
			if stale {
				return Result{}, errors.New("native defaults changed; review Defaults before Create")
			}
			if strings.TrimSpace(input.Title) == "" {
				return Result{}, errors.New("title must contain text")
			}
			id := wire.NewID()
			thread := wire.NewID()
			fields := map[string]any{"threadId": thread, "newThreadId": thread, "projectId": project.ID, "title": input.Title, "message": input.Message, "modelSelection": selected, "workspaceStrategy": d.WorkspaceStrategy, "workspaceRoot": d.WorkspaceRoot, "workspaceMode": workspaceMode, "isolated": isolated, "defaultsRevision": d.Revision, "formSource": formSource}
			bindBatchLaunch(ctx, fields)
			reply, err := a.performProjectLaunch(ctx, conn, env, fields, id, false)
			if err != nil {
				return Result{}, err
			}
			target, err := a.Target(env.ID, stringValue(reply["threadId"]))
			return Result{Next: target}, err
		default:
			return Result{}, errors.New("launch action was not offered")
		}
	}
}

func (a *Session) performProjectLaunch(ctx context.Context, conn *wire.Connection, env config.Environment, fields map[string]any, id string, retry bool) (map[string]any, error) {
	key := env.EnvironmentID + "/normal"
	a.manager.mu.Lock()
	if a.manager.closed || a.manager.inFlight[key] {
		a.manager.mu.Unlock()
		return nil, errors.New("an operation is pending or the client is closing")
	}
	previous := a.manager.State.Pending(env.EnvironmentID, false)
	if previous != nil && (!retry || stringValue(previous["id"]) != id) {
		a.manager.mu.Unlock()
		return nil, errors.New("a saved operation needs explicit recovery before launching; use /retry-launch for a saved native launch")
	}
	a.manager.inFlight[key] = true
	a.manager.operations.Add(1)
	a.manager.mu.Unlock()
	defer func() {
		a.manager.mu.Lock()
		delete(a.manager.inFlight, key)
		a.manager.mu.Unlock()
		a.manager.operations.Done()
		a.signal()
	}()
	record := map[string]any{}
	for k, v := range fields {
		record[k] = v
	}
	record["id"] = id
	record["op"] = "launch-project"
	record["environmentId"] = env.EnvironmentID
	if err := a.manager.State.SavePending(env.EnvironmentID, false, record); err != nil {
		return nil, fmt.Errorf("nothing sent: preserve launch identity: %w", err)
	}
	result, err := conn.Request(ctx, "launch-project", fields, id)
	if err != nil {
		if !retry && !wire.IsUncertain(err) {
			if clear := a.manager.State.ClearPending(env.EnvironmentID, false, id); clear != nil {
				err = errors.Join(err, clear)
			}
		}
		return nil, err
	}
	if stringValue(result["threadId"]) != stringValue(fields["newThreadId"]) {
		return nil, errors.New("launch acknowledgement identity mismatch; original operation remains saved")
	}
	if err = a.manager.State.Acknowledge(env.EnvironmentID, false, id, stringValue(fields["threadId"]), ""); err != nil {
		return nil, err
	}
	return result, nil
}

// RetryLaunch is explicit and reconciles the saved exact native identity. It
// never restarts an unresolved launch with a fresh ID or newly read defaults.
func (a *Session) RetryLaunch(ctx context.Context) (Result, error) {
	type candidate struct {
		env    config.Environment
		record map[string]any
	}
	var pending []candidate
	var choices []Choice
	for _, env := range a.manager.Environments {
		r := a.manager.State.Pending(env.EnvironmentID, false)
		if r != nil && r["op"] == "launch-project" {
			pending = append(pending, candidate{env, r})
			choices = append(choices, Choice{ID: env.ID, Label: env.Label, Description: stringValue(r["title"]) + " · " + stringValue(r["newThreadId"])})
		}
	}
	if len(pending) == 0 {
		return Result{}, errors.New("no saved native launch")
	}
	chosen := pending[0]
	if len(pending) > 1 {
		answer, err := a.asker.Ask(ctx, Question{Kind: ChooseOne, Title: "Recover launch", Choices: choices})
		if err != nil {
			return Result{}, err
		}
		if answer.Cancelled {
			return Result{}, nil
		}
		if len(answer.ChoiceIDs) != 1 {
			return Result{}, errors.New("choose one saved launch")
		}
		found := false
		for _, p := range pending {
			if p.env.ID == answer.ChoiceIDs[0] {
				chosen = p
				found = true
			}
		}
		if !found {
			return Result{}, errors.New("launch was not offered")
		}
	}
	conn, err := nativeConnection(ctx, chosen.env)
	if err != nil {
		return Result{}, err
	}
	defer closeNativeConnection(conn)
	id := stringValue(chosen.record["id"])
	delete(chosen.record, "id")
	delete(chosen.record, "op")
	delete(chosen.record, "environmentId")
	result, err := a.performProjectLaunch(ctx, conn, chosen.env, chosen.record, id, true)
	if err != nil {
		return Result{}, err
	}
	target, err := a.Target(chosen.env.ID, stringValue(result["threadId"]))
	return Result{Next: target, Text: "Original native launch reconciled."}, err
}
