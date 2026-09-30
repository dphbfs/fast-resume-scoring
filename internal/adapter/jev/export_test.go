package jev

import "time"

// SetBackoff replaces the retry backoff so tests don't sleep.
func SetBackoff(c *Client, fn func(attempt int) time.Duration) { c.backoff = fn }
