package client

import (
	"context"
	"errors"
	"fmt"
	"github.com/Xon333/t3oxide/internal/wire"
	"time"
)

// BrowseScope is a transient read scope. It never changes New's destination.
type BrowseScope struct{ ProfileID, EnvironmentID, ProjectID string }
type BrowseProjects struct {
	Scope      BrowseScope
	Projects   []NativeProject
	NextCursor *int
}

func (a *Session) BrowseProjectPage(ctx context.Context, profile string, cursor int) (BrowseProjects, error) {
	env, err := a.navigatorEnvironment(profile)
	if err != nil {
		return BrowseProjects{}, err
	}
	if cursor < 0 || cursor > 1000000 {
		return BrowseProjects{}, errors.New("invalid project cursor")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	conn, err := wire.Start(ctx, env)
	if err != nil {
		return BrowseProjects{}, err
	}
	defer closeNativeConnection(conn)
	hello, err := conn.Request(ctx, "hello", nil, "")
	if err != nil {
		return BrowseProjects{}, err
	}
	if stringValue(hello["environmentId"]) != env.EnvironmentID || numberValue(hello["protocol"]) != 1 || numberValue(hello["orchestrationProtocol"]) != 2 {
		return BrowseProjects{}, errors.New("browse helper source mismatch")
	}
	raw, err := conn.Request(ctx, "projects", map[string]any{"cursor": cursor, "limit": 200}, "")
	if err != nil {
		return BrowseProjects{}, err
	}
	projects, next, err := decodeProjects(raw)
	if err == nil && next != nil && (*next <= cursor || len(projects) == 0) {
		err = errors.New("project continuation did not advance")
	}
	return BrowseProjects{BrowseScope{profile, env.EnvironmentID, ""}, projects, next}, err
}

// ChooseBrowseScope reads one bounded project page per deliberate Next action.
func (a *Session) ChooseBrowseScope(ctx context.Context) (BrowseScope, error) {
	profiles := a.NavigatorProfiles()
	for i := range profiles {
		if env, err := a.navigatorEnvironment(profiles[i].ID); err == nil {
			profiles[i].Description = env.EnvironmentID + " · " + env.ID
		}
	}
	answer, err := a.asker.Ask(ctx, Question{Kind: ChooseOne, Title: "Browse environment", Choices: profiles})
	if err != nil {
		return BrowseScope{}, err
	}
	if answer.Cancelled {
		return BrowseScope{}, context.Canceled
	}
	if len(answer.ChoiceIDs) != 1 {
		return BrowseScope{}, errors.New("choose one offered environment")
	}
	profile := answer.ChoiceIDs[0]
	if _, err = a.navigatorEnvironment(profile); err != nil {
		return BrowseScope{}, err
	}
	cursor := 0
	for {
		page, err := a.BrowseProjectPage(ctx, profile, cursor)
		if err != nil {
			return BrowseScope{}, err
		}
		choices := []Choice{{ID: "all", Label: "All projects on this environment"}}
		for i, p := range page.Projects {
			choices = append(choices, Choice{ID: fmt.Sprintf("project:%d", i), Label: p.Title, Description: p.ID})
		}
		if page.NextCursor != nil {
			choices = append(choices, Choice{ID: "next", Label: "Next native project page"})
		}
		answer, err = a.asker.Ask(ctx, Question{Kind: ChooseOne, Title: "Browse project", Prompt: "Loaded metadata only; this does not change New or establish complete project server search.", Choices: choices})
		if err != nil {
			return BrowseScope{}, err
		}
		if answer.Cancelled {
			return BrowseScope{}, context.Canceled
		}
		if len(answer.ChoiceIDs) != 1 {
			return BrowseScope{}, errors.New("choose one offered project")
		}
		id := answer.ChoiceIDs[0]
		if id == "all" {
			return page.Scope, nil
		}
		if id == "next" && page.NextCursor != nil {
			cursor = *page.NextCursor
			continue
		}
		for i, p := range page.Projects {
			if id == fmt.Sprintf("project:%d", i) {
				page.Scope.ProjectID = p.ID
				return page.Scope, nil
			}
		}
		return BrowseScope{}, errors.New("project was not offered")
	}
}

func FilterBrowsePage(page NavigatorPage, scope BrowseScope) (NavigatorPage, error) {
	if page.ProfileID != scope.ProfileID || page.EnvironmentID != scope.EnvironmentID {
		return NavigatorPage{}, errors.New("browse source changed")
	}
	rows := []Thread{}
	for _, row := range page.Page.Threads {
		if scope.ProjectID == "" || row.ProjectID == scope.ProjectID {
			rows = append(rows, row)
		}
	}
	page.Page.Threads = rows
	return page, nil
}

func (a *Session) OpenBrowsePage(ctx context.Context, page NavigatorPage, choices []Choice) (Result, error) {
	// Derive offers from the exact page, never trust caller-provided identities.
	choices = nil
	for _, row := range page.Page.Threads {
		choices = append(choices, Choice{ID: row.ID, Label: row.Title, Description: row.Project})
	}
	if len(choices) == 0 {
		return Result{Text: "No matching conversations in this bounded metadata page; use Conversations for other pages or explicit server search."}, nil
	}
	answer, err := a.asker.Ask(ctx, Question{Kind: ChooseOne, Title: "Conversations · loaded bounded page", Prompt: "Only this metadata page is shown. Use Conversations for further pages or deliberate server search.", Choices: choices})
	if err != nil || answer.Cancelled {
		return Result{}, err
	}
	if len(answer.ChoiceIDs) != 1 {
		return Result{}, errors.New("choose one offered conversation")
	}
	next, err := a.NavigatorOpenReviewed(ctx, page, answer.ChoiceIDs[0])
	return Result{Next: next}, err
}
