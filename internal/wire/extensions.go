package wire

import (
	"encoding/json"
	"errors"

	"github.com/Xon333/t3oxide/internal/config"
)

type Lineage struct {
	RelationshipToParent *string `json:"relationshipToParent"`
	ParentThreadID       *string `json:"parentThreadId"`
	RootThreadID         *string `json:"rootThreadId"`
}

func (l *Lineage) Type() string {
	if l == nil {
		return "unknown"
	}
	if l.RelationshipToParent != nil {
		switch *l.RelationshipToParent {
		case "subagent", "fork":
			return *l.RelationshipToParent
		default:
			return "unknown"
		}
	}
	if l.ParentThreadID == nil {
		return "main"
	}
	return "unknown"
}
func (l *Lineage) Validate() error {
	if l == nil {
		return nil
	}
	for _, id := range []*string{l.ParentThreadID, l.RootThreadID} {
		if id != nil && !identifier(*id, MaxIdentifierBytes) {
			return errors.New("invalid lineage identity")
		}
	}
	if l.RelationshipToParent != nil && *l.RelationshipToParent != "subagent" && *l.RelationshipToParent != "fork" {
		return errors.New("unknown lineage relationship")
	}
	return nil
}

type RuntimeDetails struct {
	Scope                     string                 `json:"scope"`
	RunID                     *string                `json:"runId"`
	RunMessageID              *string                `json:"runMessageId,omitempty"`
	ProviderThreadID          *string                `json:"providerThreadId"`
	ProviderSessionID         *string                `json:"providerSessionId"`
	Status                    string                 `json:"status"`
	RunModelSelection         *config.ModelSelection `json:"runModelSelection"`
	ProviderReportedSelection *config.ModelSelection `json:"providerReportedSelection"`
	ProviderReportedAt        *string                `json:"providerReportedAt"`
	SessionReportedModel      *string                `json:"sessionReportedModel"`
}

func decodeExtensions(o object, m *Metadata) error {
	if raw := o["modelSelection"]; len(raw) > 0 && string(raw) != "null" {
		m.ModelSelection = &config.ModelSelection{}
		if json.Unmarshal(raw, m.ModelSelection) != nil {
			return fieldError("modelSelection")
		}
		if err := m.ModelSelection.Validate(); err != nil {
			return err
		}
	}
	if raw := o["lineage"]; len(raw) > 0 && string(raw) != "null" {
		// Omitted fields are unknown, not evidence of a root thread.
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil {
			return fieldError("lineage")
		}
		if _, ok := fields["relationshipToParent"]; ok {
			if _, ok = fields["parentThreadId"]; ok {
				m.Lineage = &Lineage{}
				if json.Unmarshal(raw, m.Lineage) != nil {
					return fieldError("lineage")
				}
				if err := m.Lineage.Validate(); err != nil {
					return err
				}
			}
		}
	}
	if raw := o["runtimeDetails"]; len(raw) > 0 && string(raw) != "null" {
		if len(raw) > 64*1024 {
			return fieldError("runtimeDetails")
		}
		m.RuntimeDetails = &RuntimeDetails{}
		if json.Unmarshal(raw, m.RuntimeDetails) != nil {
			return fieldError("runtimeDetails")
		}
		r := m.RuntimeDetails
		switch r.Scope {
		case "active-run", "last-run", "provider-thread", "unreported":
		default:
			return fieldError("runtimeDetails.scope")
		}
		for _, id := range []*string{r.RunID, r.RunMessageID, r.ProviderThreadID, r.ProviderSessionID} {
			if id != nil && !identifier(*id, MaxIdentifierBytes) {
				return fieldError("runtimeDetails.id")
			}
		}
		if len(r.Status) > 64 || r.ProviderReportedAt != nil && len(*r.ProviderReportedAt) > 128 || r.SessionReportedModel != nil && len(*r.SessionReportedModel) > 256 {
			return fieldError("runtimeDetails.text")
		}
		for _, s := range []*config.ModelSelection{r.RunModelSelection, r.ProviderReportedSelection} {
			if s != nil {
				if err := s.Validate(); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
