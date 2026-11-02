package client

import (
	"github.com/Xon333/t3oxide/internal/wire"
	"testing"
)

func TestCatalogIdentityLabelUsesOnlyMatchingReportedProvider(t *testing.T) {
	c := ModelCatalog{InstanceID: "pro-instance", ProviderLabel: "Host Pro"}
	if got := c.ProviderSummary(); got != "Host Pro [pro-instance]" {
		t.Fatal(got)
	}
	c.ProviderIdentity = &wire.ProviderIdentity{InstanceID: "different", Label: "Wrong account"}
	if got := c.ProviderSummary(); got != "Host Pro [pro-instance]" {
		t.Fatal("mismatched identity: " + got)
	}
	c.ProviderIdentity = &wire.ProviderIdentity{InstanceID: "pro-instance", Label: "Native Pro"}
	if got := c.ProviderSummary(); got != "Native Pro [pro-instance]" {
		t.Fatal(got)
	}
}
