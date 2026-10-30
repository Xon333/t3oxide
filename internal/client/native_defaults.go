package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Xon333/t3oxide/internal/config"
	"github.com/Xon333/t3oxide/internal/wire"
	"strings"
	"time"
)

type NativeDefaultsField struct {
	Key            string `json:"key"`
	Source         string `json:"source"`
	Overridden     bool   `json:"overridden"`
	Value          any    `json:"value"`
	InheritedValue any    `json:"inheritedValue"`
}

type NativeDefaults struct {
	ProjectID              string                 `json:"projectId"`
	ProjectTitle           string                 `json:"projectTitle"`
	Scope                  string                 `json:"scope"`
	Revision               string                 `json:"revision"`
	WriteSupported         bool                   `json:"writeSupported"`
	WriteReason            string                 `json:"writeReason"`
	SettingsRoute          string                 `json:"settingsRoute"`
	Fields                 []NativeDefaultsField  `json:"fields"`
	StoredModelSelection   *config.ModelSelection `json:"storedModelSelection"`
	ModelAvailable         bool                   `json:"modelAvailable"`
	ModelUnavailableReason string                 `json:"modelUnavailableReason"`
	ProviderIdentity       *wire.ProviderIdentity `json:"providerIdentity"`
	DesktopLocal           string                 `json:"desktopLocal"`
}

func DecodeNativeDefaults(result map[string]any, project, scope string) (NativeDefaults, error) {
	var v NativeDefaults
	raw, err := json.Marshal(result)
	if err != nil || len(raw) > 256*1024 {
		return v, errors.New("native defaults exceed bounded budget")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if d.Decode(&v) != nil || v.ProjectID != project || v.Scope != scope || scope != "project" && scope != "environment" || len(v.ProjectTitle) > 512 || len(v.Revision) != 64 || strings.Trim(v.Revision, "0123456789abcdef") != "" || len(v.WriteReason) > 4096 || len(v.ModelUnavailableReason) > 4096 || len(v.SettingsRoute) > 2048 || len(v.Fields) > 16 || v.DesktopLocal != "unreported" {
		return v, errors.New("invalid scoped native defaults")
	}
	// No safe native update/CAS/idempotency contract is present in the pinned
	// adapter. A future capability must bring a versioned write implementation;
	// an unexpected flag alone never grants mutation authority.
	if v.WriteSupported {
		return v, errors.New("native defaults write capability is newer than this verified client contract; use native Settings")
	}
	if v.WriteReason == "" {
		return v, errors.New("native defaults missing precise write limitation")
	}
	if v.StoredModelSelection != nil {
		if err = v.StoredModelSelection.Validate(); err != nil {
			return v, err
		}
	}
	if v.ProviderIdentity != nil {
		if err = v.ProviderIdentity.Validate(); err != nil {
			return v, err
		}
	}
	seen := map[string]bool{}
	for _, f := range v.Fields {
		if seen[f.Key] || len(f.Key) == 0 || len(f.Key) > 128 || len(f.Source) > 256 {
			return v, errors.New("invalid native defaults field")
		}
		seen[f.Key] = true
		// Unknown future read-only fields remain bounded and visible. Their
		// presence cannot enable a write or change a stored native selection.
		for _, value := range []any{f.Value, f.InheritedValue} {
			encoded, e := json.Marshal(value)
			if e != nil || len(encoded) > 64*1024 {
				return v, errors.New("native defaults field exceeds budget")
			}
		}
	}
	return v, nil
}

// NativeDefaults is an explicit bounded settings read. No mutation or watch
// occurs and the active conversation/draft/Stop target remains unchanged.
func (a *Session) NativeDefaults(ctx context.Context, profile, project, scope string) (NativeDefaults, error) {
	env, err := a.navigatorEnvironment(profile)
	if err != nil {
		return NativeDefaults{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	conn, err := wire.Start(ctx, env)
	if err != nil {
		return NativeDefaults{}, err
	}
	defer closeNativeConnection(conn)
	hello, err := conn.Request(ctx, "hello", nil, "")
	if err != nil {
		return NativeDefaults{}, err
	}
	if stringValue(hello["environmentId"]) != env.EnvironmentID || numberValue(hello["protocol"]) != 1 || numberValue(hello["orchestrationProtocol"]) != 2 {
		return NativeDefaults{}, errors.New("native defaults helper identity or protocol mismatch")
	}
	features, _ := hello["features"].(map[string]any)
	if !boolValue(features["nativeDefaults"]) {
		return NativeDefaults{}, errors.New("native defaults read capability is unavailable; open the selected environment's native T3 Settings")
	}
	result, err := conn.Request(ctx, "native-defaults", map[string]any{"projectId": project, "scope": scope}, "")
	if err != nil {
		return NativeDefaults{}, err
	}
	return DecodeNativeDefaults(result, project, scope)
}

func nativeDefaultsScopeLabel(scope string) string {
	if scope == "environment" {
		return "Environment"
	}
	return "Project on environment"
}

func nativeDefaultsText(v NativeDefaults, environment string) string {
	var out strings.Builder
	scope := nativeDefaultsScopeLabel(v.Scope)
	fmt.Fprintf(&out, "Scope: %s\nEnvironment: %s\nProject: %s · %s\n\nEditing unavailable: %s\nNative Settings: %s\n\n", scope, environment, v.ProjectTitle, v.ProjectID, v.WriteReason, v.SettingsRoute)
	for _, f := range v.Fields {
		value, _ := json.MarshalIndent(f.Value, "", "  ")
		fmt.Fprintf(&out, "%s\nSource: %s\n%s\n", f.Key, f.Source, string(value))
		if f.Overridden {
			inherited, _ := json.Marshal(f.InheritedValue)
			fmt.Fprintf(&out, "Project override. Reset would remove only this field and follow: %s\n", inherited)
		}
		out.WriteString("\n")
	}
	if !v.ModelAvailable {
		fmt.Fprintf(&out, "Saved selection unavailable: %s. No other provider/account is substituted.\n", v.ModelUnavailableReason)
	}
	fmt.Fprintf(&out, "Desktop-local defaults: unreported\nRevision: %s\nThis-launch controls remain separate; they do not save persistent defaults.\n", v.Revision)
	return out.String()
}

func (a *Session) EditNativeDefaults(ctx context.Context, profile, project string) (Result, error) {
	if profile == "" {
		env, err := a.creationEnvironment(ctx)
		if err != nil {
			return Result{}, err
		}
		profile = env.ID
		if pin, ok := a.CreationDestination(); ok && pin.ProfileID == profile {
			project = pin.ProjectID
		}
	}
	env, err := a.navigatorEnvironment(profile)
	if err != nil {
		return Result{}, err
	}
	if project == "" {
		conn, err := nativeConnection(ctx, env)
		if err != nil {
			return Result{}, err
		}
		p, err := a.chooseNativeProject(ctx, conn, "")
		closeNativeConnection(conn)
		if errors.Is(err, context.Canceled) && ctx.Err() == nil {
			return Result{}, nil
		}
		if err != nil {
			return Result{}, err
		}
		project = p.ID
	}
	scope := "project"
	for {
		defaults, err := a.NativeDefaults(ctx, profile, project, scope)
		if err != nil {
			return Result{}, err
		}
		nextScope := "environment"
		label := "Environment"
		if scope == "environment" {
			nextScope = "project"
			label = "Project"
		}
		answer, err := a.asker.Ask(ctx, Question{Surface: SurfaceNativeDefaults, Kind: ChooseOne, Title: nativeDefaultsScopeLabel(scope) + " defaults · " + env.Label, Prompt: nativeDefaultsText(defaults, env.EnvironmentID), Choices: []Choice{{ID: "scope", Label: label, Description: "Read " + nextScope + " defaults on this exact environment"}, {ID: "refresh", Label: "Refresh"}, {ID: "close", Label: "Back"}}})
		if err != nil || answer.Cancelled {
			return Result{}, err
		}
		if len(answer.ChoiceIDs) != 1 {
			return Result{}, errors.New("choose one offered defaults action")
		}
		switch answer.ChoiceIDs[0] {
		case "scope":
			scope = nextScope
		case "refresh":
		case "close":
			return Result{}, nil
		default:
			return Result{}, errors.New("defaults action was not offered")
		}
	}
}
