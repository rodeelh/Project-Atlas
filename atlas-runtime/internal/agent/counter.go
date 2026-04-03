package agent

import "sync/atomic"

// Session-wide token accumulators. Incremented atomically on every AI call.
var sessionIn, sessionOut int64

// AddSessionTokens adds input and output token counts to the running process totals.
// Safe to call from multiple goroutines.
func AddSessionTokens(in, out int) {
	atomic.AddInt64(&sessionIn, int64(in))
	atomic.AddInt64(&sessionOut, int64(out))
}

// GetSessionTokens returns the cumulative input and output token counts
// since process start.
func GetSessionTokens() (in, out int64) {
	return atomic.LoadInt64(&sessionIn), atomic.LoadInt64(&sessionOut)
}
