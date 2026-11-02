package ui

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/Xon333/t3oxide/internal/wire"
	"github.com/bluekeyes/go-gitdiff/gitdiff"
)

const reviewIndexMaxBytes = 128*1024 + 8
const reviewIndexMaxFiles = 200
const reviewIndexMaxHunks = 2048

// reviewIndex is a detached presentation snapshot. Treat it as immutable. Digest
// invalidates note anchors, not human note text; callers must also qualify it by
// environment/profile. GeneratedAt is deliberately excluded from the digest.
type reviewIndex struct {
	Digest  string
	Sources []wire.ReviewSource
	Files   []reviewIndexFile
	Raw     string
	Clipped bool
}

type reviewIndexFile struct {
	Path, OldPath, NewPath string
	Raw                    string
	Fallback               string
	// Source is -1 when aggregate ThreadReview cannot establish source ownership.
	// The wire contract concatenates diffs without source byte boundaries.
	Source int
	Hunks  []reviewIndexHunk
}

type reviewIndexHunk struct {
	Header, Text                           string
	OldStart, OldLines, NewStart, NewLines int64
}

func buildReviewIndex(review wire.ThreadReview) reviewIndex {
	// Hash original evidence, including clipping and source order/hash. Never hash
	// derived display labels or observation time in place of native evidence.
	evidence := struct {
		Thread, Project, Revision, Workspace, Branch, Diff, Unavailable string
		Clipped                                                         bool
		Sources                                                         []wire.ReviewSource
		Files                                                           []wire.ReviewFile
	}{review.ThreadID, review.ProjectID, review.Revision, review.Workspace, review.Branch, review.Diff, review.Unavailable, review.DiffTruncated, review.Sources, review.Files}
	encoded, _ := json.Marshal(evidence)
	sum := sha256.Sum256(encoded)
	index := reviewIndex{Digest: hex.EncodeToString(sum[:]), Sources: append([]wire.ReviewSource(nil), review.Sources...), Raw: review.Diff, Clipped: review.DiffTruncated}
	if len(index.Raw) > reviewIndexMaxBytes {
		index.Raw = index.Raw[:reviewIndexMaxBytes]
		for !utf8.ValidString(index.Raw) && len(index.Raw) > 0 {
			index.Raw = index.Raw[:len(index.Raw)-1]
		}
		index.Clipped = true
	}
	for _, s := range index.Sources {
		index.Clipped = index.Clipped || s.Truncated
	}
	source := -1
	if len(index.Sources) == 1 {
		source = 0
	}
	// Split only on Git file boundaries. Unknown/unified-only input remains raw;
	// it must never disappear merely because a parser found no files.
	starts := []int{}
	if strings.HasPrefix(index.Raw, "diff --git ") {
		starts = append(starts, 0)
	}
	for at := 0; at < len(index.Raw); {
		n := strings.Index(index.Raw[at:], "\ndiff --git ")
		if n < 0 {
			break
		}
		at += n + 1
		starts = append(starts, at)
	}
	appendRaw := func(raw, reason string) {
		index.Files = append(index.Files, reviewIndexFile{Path: "Raw diff", Raw: raw, Fallback: reason, Source: source})
	}
	if len(starts) == 0 {
		if index.Raw != "" {
			appendRaw(index.Raw, "unstructured diff")
		}
		return index
	}
	if starts[0] > 0 {
		appendRaw(index.Raw[:starts[0]], "unstructured preamble")
	}
	hunks := 0
	for i, start := range starts {
		end := len(index.Raw)
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		if len(index.Files) >= reviewIndexMaxFiles-1 && i+1 < len(starts) {
			appendRaw(index.Raw[start:], "file limit")
			index.Clipped = true
			break
		}
		raw := index.Raw[start:end]
		row := reviewIndexFile{Path: "Raw diff", Raw: raw, Source: source}
		// Do not feed binary bodies to the parser, even malformed ones. Parsing is
		// presentation only; patch application and binary decoding are never used.
		if strings.HasPrefix(raw, "GIT binary patch") || strings.Contains(raw, "\nGIT binary patch") || strings.Contains(raw, "\nBinary files ") {
			row.Fallback = "binary diff"
		} else {
			files, pre, err := gitdiff.Parse(strings.NewReader(raw))
			if err != nil || len(files) != 1 || pre != "" {
				row.Fallback = "malformed diff"
			} else {
				f := files[0]
				row.OldPath = f.OldName
				row.NewPath = f.NewName
				row.Path = f.NewName
				if row.Path == "" {
					row.Path = f.OldName
				}
				switch {
				case f.IsBinary:
					row.Fallback = "binary diff"
				case f.IsRename || f.IsCopy:
					row.Fallback = "rename/copy diff"
				case len(f.TextFragments) == 0:
					row.Fallback = "mode/metadata diff"
				default:
					for _, h := range f.TextFragments {
						if h.Validate() != nil {
							row.Fallback = "malformed hunk"
							row.Hunks = nil
							break
						}
						if hunks >= reviewIndexMaxHunks {
							row.Fallback = "hunk limit"
							break
						}
						row.Hunks = append(row.Hunks, reviewIndexHunk{Header: h.Header(), Text: h.String(), OldStart: h.OldPosition, OldLines: h.OldLines, NewStart: h.NewPosition, NewLines: h.NewLines})
						hunks++
					}
				}
			}
		}
		if index.Clipped {
			row.Fallback = "clipped diff"
		}
		index.Files = append(index.Files, row)
	}
	return index
}
