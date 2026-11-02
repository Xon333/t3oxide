package ui

import (
	"strings"

	"github.com/Xon333/t3oxide/internal/wire"
)

func reported(value string) string {
	if value == "" {
		return "unreported"
	}
	return value
}
func providerIdentityLines(identity *wire.ProviderIdentity) []string {
	if identity == nil {
		return []string{"Provider instance: unreported", "Account: unreported"}
	}
	return []string{"Provider instance: " + reported(identity.InstanceID), "Provider label: " + reported(identity.Label), "Provider driver: " + reported(identity.Driver), "Account: " + reported(identity.AccountLabel), "Account telemetry observed: " + reported(identity.AccountReportedAt), "A native instance ID identifies configuration, not a person or account."}
}

func (m *Model) providerModelStatus() string {
	parts := []string{}
	if m.snapshot != nil && m.snapshot.Metadata.ProviderIdentity != nil {
		identity := m.snapshot.Metadata.ProviderIdentity
		for _, value := range []string{identity.Label, identity.InstanceID} {
			if value != "" && (len(parts) == 0 || parts[len(parts)-1] != safeText(value)) {
				parts = append(parts, safeText(value))
			}
		}
	}
	parts = append(parts, modelStatusLine(m.status.ModelName))
	return strings.Join(parts, " · ")
}
