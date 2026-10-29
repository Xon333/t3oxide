package config

import (
	"errors"
	"strings"
)

// A batch is an editable human list, never a dispatch queue. The native launch
// acknowledgement advances its exact unchanged prefix in the same state
// transaction that retires the operation journal, including RetryLaunch.
func advanceBatchDraft(next *savedState, record map[string]any) error {
	value, exists := record["batchDraft"]
	if !exists {
		return nil
	}
	b, ok := value.(map[string]any)
	if !ok {
		return errors.New("invalid saved batch draft binding")
	}
	env, e := b["environment"].(string)
	source, s := b["source"].(string)
	before, f := b["before"].(string)
	consumed, c := b["consumed"].(string)
	if !e || !s || !f || !c || env == "" || len(env) > 1024 || !strings.HasPrefix(source, "\x00file-many/") || len(source) > 4096 || len(before) > 32*1024 || consumed == "" || !strings.HasPrefix(before, consumed) || consumed != before && !strings.HasSuffix(consumed, "\n") {
		return errors.New("invalid saved batch draft binding")
	}
	count := 0
	for _, line := range strings.Split(consumed, "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	if count != 1 {
		return errors.New("batch receipt must consume exactly one reviewed human task")
	}
	key := draftKey(env, source)
	if next.Drafts[key] != before {
		return nil
	}
	rest := strings.TrimPrefix(before, consumed)
	if strings.TrimSpace(rest) == "" {
		delete(next.Drafts, key)
	} else {
		next.Drafts[key] = rest
	}
	return nil
}
