package client

import (
	"context"
	"strings"
	"testing"
)

func TestExplicitBatchParsingPreservesBriefBytesAndIndependentFormDrafts(t *testing.T) {
	const input = "\n  task one  \n\n界 second task  \n"
	lines, err := parseFileTaskLines(input)
	if err != nil || len(lines) != 2 {
		t.Fatal(lines, err)
	}
	if lines[0].Message != "  task one  " || lines[0].Consumed != "\n  task one  \n" || lines[1].Message != "界 second task  " || lines[1].Consumed != "\n界 second task  \n" {
		t.Fatal("batch parser changed human bytes")
	}
	base := "project-launch/project"
	a := context.WithValue(context.Background(), batchLaunchKey{}, &batchLaunchDraft{Source: "source", Before: input})
	b := context.WithValue(context.Background(), batchLaunchKey{}, &batchLaunchDraft{Source: "source", Before: lines[1].Consumed})
	if batchLaunchFormSource(a, base) == base || batchLaunchFormSource(a, base) == batchLaunchFormSource(b, base) {
		t.Fatal("batch form overwrote another pending form")
	}
	for _, bad := range []string{" \n", strings.Repeat("task\n", 21), strings.Repeat("a", 8193), string([]byte{0xff})} {
		if _, err = parseFileTaskLines(bad); err == nil {
			t.Fatal("invalid batch accepted")
		}
	}
}
