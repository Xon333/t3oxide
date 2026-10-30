package wire

import (
	"errors"
	"strings"
)

// ProviderIdentity distinguishes a native instance from reported account
// telemetry. Empty account fields mean unreported; an instance is not a person.
type ProviderIdentity struct {
	InstanceID        string `json:"instanceId"`
	Driver            string `json:"driver"`
	Label             string `json:"label"`
	AccountLabel      string `json:"accountLabel"`
	AccountReportedAt string `json:"accountReportedAt"`
}

func (v ProviderIdentity) Validate() error {
	if v.InstanceID != "" && !identifier(v.InstanceID, 256) || len(v.Driver) > 64 || len(v.Label) > 256 || len(v.AccountLabel) > 256 || len(v.AccountReportedAt) > 64 || strings.ContainsAny(v.Driver, "\x00\r\n") || !nativeDate(v.AccountReportedAt, true) {
		return errors.New("invalid bounded provider identity")
	}
	return nil
}
