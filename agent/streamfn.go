package agent

import (
	"errors"
	"sync"

	"github.com/mikus/maiku/ai"
)

var (
	defaultStreamFnMu sync.RWMutex
	defaultStreamFn   ai.StreamFn
)

// SetDefaultStreamFn configures the fallback used by Agent and the low-level
// loop functions when callers omit a StreamFn.
//
// Hosts that provide a default model runtime can install its stream function
// here without making this package depend on a provider catalog.
func SetDefaultStreamFn(fn ai.StreamFn) {
	defaultStreamFnMu.Lock()
	defer defaultStreamFnMu.Unlock()
	defaultStreamFn = fn
}

// GetDefaultStreamFn returns the configured default stream function, or an
// error if none has been configured.
func GetDefaultStreamFn() (ai.StreamFn, error) {
	defaultStreamFnMu.RLock()
	defer defaultStreamFnMu.RUnlock()
	if defaultStreamFn == nil {
		return nil, errors.New("agent: no default stream function configured; pass streamFn explicitly or call SetDefaultStreamFn()")
	}
	return defaultStreamFn, nil
}
