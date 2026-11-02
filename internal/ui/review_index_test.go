package ui

import (
	"strings"
	"testing"

	"github.com/Xon333/t3oxide/internal/wire"
)

const reviewIndexFixture = "diff --git a/a.txt b/a.txt\n--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-old\n+new\n"

func TestReviewIndexHunksAndDuplicatePaths(t *testing.T) {
	r := wire.ThreadReview{Diff: reviewIndexFixture + reviewIndexFixture, Sources: []wire.ReviewSource{{Kind: "working-tree", Hash: "one"}, {Kind: "branch-range", Hash: "two"}}}
	idx := buildReviewIndex(r)
	if len(idx.Files) != 2 {
		t.Fatalf("files: %#v", idx.Files)
	}
	for _, f := range idx.Files {
		if f.Path != "a.txt" || f.Source != -1 || f.Fallback != "" || len(f.Hunks) != 1 {
			t.Fatalf("file: %#v", f)
		}
		h := f.Hunks[0]
		if h.OldStart != 1 || h.NewStart != 1 || h.OldLines != 1 || h.NewLines != 1 || !strings.Contains(h.Text, "+new") {
			t.Fatalf("hunk: %#v", h)
		}
	}
	if idx.Raw != r.Diff {
		t.Fatal("raw changed")
	}
}

func TestReviewIndexFallbacks(t *testing.T) {
	for _, tc := range []struct{ name, diff, reason string }{
		{"malformed", "diff --git a/a b/a\n--- a/a\n+++ b/a\n@@ -1,99 +1 @@\n-x\n+y\n", "malformed diff"},
		{"binary", "diff --git a/a b/a\nGIT binary patch\nliteral 999999999999999\ninvalid payload\n", "binary diff"},
		{"binary summary", "diff --git a/a b/a\nBinary files a/a and b/a differ\n", "binary diff"},
		{"rename", "diff --git a/a b/b\nsimilarity index 100%\nrename from a\nrename to b\n", "rename/copy diff"},
		{"mode", "diff --git a/a b/a\nold mode 100644\nnew mode 100755\n", "mode/metadata diff"},
		{"unknown", "not a patch\n", "unstructured diff"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			idx := buildReviewIndex(wire.ThreadReview{Diff: tc.diff})
			if len(idx.Files) != 1 || idx.Files[0].Fallback != tc.reason || idx.Files[0].Raw != tc.diff {
				t.Fatalf("%#v", idx)
			}
		})
	}
}

func TestReviewIndexDigestAndSnapshot(t *testing.T) {
	r := wire.ThreadReview{Diff: reviewIndexFixture, Sources: []wire.ReviewSource{{Kind: "working-tree", Hash: "one"}}}
	base := buildReviewIndex(r)
	r.GeneratedAt = "later"
	if buildReviewIndex(r).Digest != base.Digest {
		t.Fatal("observation time invalidated anchors")
	}
	r.Sources[0].Hash = "two"
	if base.Sources[0].Hash != "one" || buildReviewIndex(r).Digest == base.Digest {
		t.Fatal("source evidence not detached/hashed")
	}
	for _, mutate := range []func(*wire.ThreadReview){
		func(r *wire.ThreadReview) { r.Diff += "\n" },
		func(r *wire.ThreadReview) { r.DiffTruncated = true },
		func(r *wire.ThreadReview) { r.ThreadID = "other" },
		func(r *wire.ThreadReview) { r.Sources[0].Truncated = true },
	} {
		before := buildReviewIndex(r).Digest
		copy := r
		copy.Sources = append([]wire.ReviewSource(nil), r.Sources...)
		mutate(&copy)
		if buildReviewIndex(copy).Digest == before {
			t.Fatal("changed evidence kept digest")
		}
	}
}

func TestReviewIndexBudgetsAndClipping(t *testing.T) {
	r := wire.ThreadReview{Diff: strings.Repeat(reviewIndexFixture, 500)}
	idx := buildReviewIndex(r)
	if len(idx.Files) > reviewIndexMaxFiles || !idx.Clipped || !strings.Contains(idx.Files[len(idx.Files)-1].Fallback, "limit") {
		t.Fatalf("file bound: %d", len(idx.Files))
	}
	idx = buildReviewIndex(wire.ThreadReview{Diff: strings.Repeat("x", reviewIndexMaxBytes+100)})
	if len(idx.Raw) > reviewIndexMaxBytes || !idx.Clipped {
		t.Fatal("byte bound")
	}
	idx = buildReviewIndex(wire.ThreadReview{Diff: reviewIndexFixture, DiffTruncated: true})
	if idx.Files[0].Fallback != "clipped diff" || idx.Files[0].Raw != reviewIndexFixture {
		t.Fatal("lost clipped evidence")
	}
	idx = buildReviewIndex(wire.ThreadReview{})
	if len(idx.Files) != 0 || idx.Digest == "" {
		t.Fatal("empty review")
	}
}

func TestReviewIndexDeletionAndQuotedPath(t *testing.T) {
	for _, tc := range []struct{ diff, path string }{
		{"diff --git a/gone b/gone\ndeleted file mode 100644\n--- a/gone\n+++ /dev/null\n@@ -1 +0,0 @@\n-old\n", "gone"},
		{"diff --git \"a/a\\tb\" \"b/a\\tb\"\n--- \"a/a\\tb\"\n+++ \"b/a\\tb\"\n@@ -1 +1 @@\n-old\n+new\n\\ No newline at end of file\n", "a\tb"},
	} {
		idx := buildReviewIndex(wire.ThreadReview{Diff: tc.diff, Sources: []wire.ReviewSource{{Kind: "working-tree"}}})
		if len(idx.Files) != 1 || idx.Files[0].Path != tc.path || idx.Files[0].Source != 0 || len(idx.Files[0].Hunks) != 1 || idx.Files[0].Raw != tc.diff {
			t.Fatalf("%#v", idx.Files)
		}
	}
}
