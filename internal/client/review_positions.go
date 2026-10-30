package client

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/Xon333/t3oxide/internal/config"
	"github.com/Xon333/t3oxide/internal/wire"
)

func reviewPositionSource(target InboxTarget) config.ReviewPosition {
	return config.ReviewPosition{ProfileID: target.ProfileID, EnvironmentID: target.EnvironmentID, ProjectID: target.ProjectID, ThreadID: target.ThreadID}
}

func reviewPositionDigest(target InboxTarget, review wire.ThreadReview) (string, string) {
	if review.ThreadID != target.ThreadID || review.ProjectID != target.ProjectID {
		return "", "Review source changed; starting at the beginning."
	}
	if review.Unavailable != "" {
		return "", "Review unavailable; no saved position restored."
	}
	if review.DiffTruncated {
		return "", "Changes are clipped; no saved position restored."
	}
	if len(review.Sources) == 0 {
		return "", "Diff hashes unreported; starting at the beginning."
	}
	type sourceHash struct{ Kind, Hash string }
	hashes := make([]sourceHash, 0, len(review.Sources))
	for _, source := range review.Sources {
		if source.Truncated || source.Hash == "" {
			return "", "Diff hashes missing or clipped; starting at the beginning."
		}
		hashes = append(hashes, sourceHash{source.Kind, source.Hash})
	}
	data, _ := json.Marshal(hashes)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), ""
}

// ReviewPosition returns a bounded viewport coordinate only for the exact
// profile/environment/project/thread and complete native diff hashes.
func (a *Session) ReviewPosition(target InboxTarget, review wire.ThreadReview) (int, string) {
	if a == nil || a.manager == nil || a.manager.State == nil {
		return 0, ""
	}
	digest, reason := reviewPositionDigest(target, review)
	if digest == "" {
		return 0, reason
	}
	position, ok := a.manager.State.ReviewPosition(reviewPositionSource(target))
	if !ok {
		return 0, ""
	}
	if position.Digest != digest {
		return 0, "Changes updated; review again from the beginning."
	}
	return position.Offset, "Restored local reading position for these unchanged diff hashes."
}

func (a *Session) SaveReviewPosition(target InboxTarget, review wire.ThreadReview, offset int) error {
	if a == nil || a.manager == nil || a.manager.State == nil {
		return errors.New("review position storage unavailable")
	}
	position := reviewPositionSource(target)
	position.Digest, _ = reviewPositionDigest(target, review)
	position.Offset = offset
	return a.manager.State.SaveReviewPosition(position)
}
