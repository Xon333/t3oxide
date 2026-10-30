package client

import (
	"context"
	"errors"
	"fmt"

	"github.com/Xon333/t3oxide/internal/config"
)

func (a *Session) CreationDestination() (config.CreationDestination, bool) {
	if a == nil || a.manager == nil || a.manager.State == nil {
		return config.CreationDestination{}, false
	}
	return a.manager.State.CreationDestination()
}

// CreationDestinationHint reports the same environment precedence as New using
// configured identity only. It does not contact a host or import thread defaults.
// An unavailable explicit/pinned identity stays visible and requires correction.
func (a *Session) CreationDestinationHint() (config.CreationDestination, string) {
	if a == nil || a.manager == nil {
		return config.CreationDestination{}, "choose"
	}
	if profile := a.manager.CreationProfile; profile != "" {
		for _, env := range a.manager.Environments {
			if env.ID == profile {
				return config.CreationDestination{ProfileID: env.ID, EnvironmentID: env.EnvironmentID}, "explicit"
			}
		}
		return config.CreationDestination{ProfileID: profile}, "explicit-unavailable"
	}
	if pin, ok := a.CreationDestination(); ok {
		for _, env := range a.manager.Environments {
			if env.ID == pin.ProfileID && env.EnvironmentID == pin.EnvironmentID {
				return pin, "pinned"
			}
		}
		return pin, "pinned-unavailable"
	}
	var configured *config.Environment
	for _, env := range a.manager.Environments {
		if env.DefaultForNew {
			if configured != nil {
				return config.CreationDestination{}, "conflicting-defaults"
			}
			copy := env
			configured = &copy
		}
	}
	if configured != nil {
		return config.CreationDestination{ProfileID: configured.ID, EnvironmentID: configured.EnvironmentID}, "configured"
	}
	if len(a.manager.Environments) == 1 {
		env := a.manager.Environments[0]
		return config.CreationDestination{ProfileID: env.ID, EnvironmentID: env.EnvironmentID}, "single"
	}
	return config.CreationDestination{}, "choose"
}

func (a *Session) PinCreationDestination(profile, environment, project string) error {
	env, err := a.navigatorEnvironment(profile)
	if err != nil {
		return err
	}
	if env.EnvironmentID != environment {
		return errors.New("creation destination environment changed; choose the exact current source")
	}
	return a.manager.State.SetCreationDestination(&config.CreationDestination{ProfileID: profile, EnvironmentID: environment, ProjectID: project})
}

func (a *Session) ClearCreationDestination() error {
	return a.manager.State.SetCreationDestination(nil)
}

// EditCreationDestination changes only the next-New target after an explicit
// choice. Browsing, attaching and pinning never copy defaults from a thread.
func (a *Session) EditCreationDestination(ctx context.Context) (Result, error) {
	choices := []Choice{{ID: "\x00reset", Label: "Reset creation destination", Description: "Use explicit CLI target, configured default, or choose when New opens"}}
	for _, env := range a.manager.Environments {
		choices = append(choices, Choice{ID: env.ID, Label: env.Label, Description: env.EnvironmentID})
	}
	answer, err := a.asker.Ask(ctx, Question{Kind: ChooseOne, Title: "Creation destination", Prompt: "Pin only the environment and project. Native model, account, options and workspace are read fresh for each New review.", Choices: choices})
	if err != nil || answer.Cancelled {
		return Result{}, err
	}
	if len(answer.ChoiceIDs) != 1 {
		return Result{}, errors.New("choose one offered destination")
	}
	if answer.ChoiceIDs[0] == "\x00reset" {
		return Result{Text: "Creation destination reset."}, a.ClearCreationDestination()
	}
	env, err := a.navigatorEnvironment(answer.ChoiceIDs[0])
	if err != nil {
		return Result{}, err
	}
	conn, err := nativeConnection(ctx, env)
	if err != nil {
		return Result{}, err
	}
	defer closeNativeConnection(conn)
	project, err := a.chooseNativeProject(ctx, conn, "")
	if errors.Is(err, context.Canceled) && ctx.Err() == nil {
		return Result{}, nil
	}
	if err != nil {
		return Result{}, err
	}
	if err = a.PinCreationDestination(env.ID, env.EnvironmentID, project.ID); err != nil {
		return Result{}, err
	}
	return Result{Text: fmt.Sprintf("New destination pinned: %s · %s", env.Label, project.Title)}, nil
}
