package ai

import "time"

// SetJitter replaces c's backoff jitter source with f, which is given the backoff's cap and
// returns the wait. It is the only way to replace it: the Client exports no hook, and the
// jitter is random, so a test can't otherwise time the backoff.
func SetJitter(c *Client, f func(time.Duration) time.Duration) { c.jitter = f }
