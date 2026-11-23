package javdb

import (
	"crypto/sha256"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/metatube-community/metatube-sdk-go/errors"
)

const (
	defaultRequestInterval = time.Minute
	defaultCooldown        = 30 * time.Minute
)

var errLoginRequired = errors.New(http.StatusUnauthorized, "JavDB requires a fresh login Cookie")

type requestGate struct {
	mu         sync.Mutex
	interval   time.Duration
	pauseUntil time.Time
	next       time.Time
	active     bool
	rejected   [sha256.Size]byte
}

func (g *requestGate) begin(cookie string, stale bool) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := time.Now()
	if now.Before(g.pauseUntil) {
		return errors.New(http.StatusServiceUnavailable, "JavDB scraping is paused until "+g.pauseUntil.Format(time.RFC3339))
	}
	if cookie != "" && (stale || g.rejected == sha256.Sum256([]byte(cookie))) {
		return errLoginRequired
	}
	if g.active || now.Before(g.next) {
		return errors.New(http.StatusTooManyRequests, "JavDB request interval or cooldown has not elapsed")
	}
	interval := g.interval
	if interval == 0 {
		interval = defaultRequestInterval
	}
	g.active = true
	g.next = now.Add(interval)
	return nil
}

func (g *requestGate) finish(cookie string, rejected bool, status int, retryAfter string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.active = false
	if cookie != "" && (rejected || status == http.StatusUnauthorized || status == http.StatusForbidden) {
		g.rejected = sha256.Sum256([]byte(cookie))
	}
	if status == http.StatusTooManyRequests || status == http.StatusForbidden || (status == http.StatusServiceUnavailable && retryAfter != "") {
		now := time.Now()
		until := maxTime(now.Add(defaultCooldown), retryAfterTime(now, retryAfter))
		g.next = maxTime(g.next, until)
	}
}

func retryAfterTime(now time.Time, value string) time.Time {
	value = strings.TrimSpace(value)
	if date, err := http.ParseTime(value); err == nil {
		return date
	}
	if seconds, err := strconv.ParseUint(value, 10, 32); err == nil {
		return now.Add(time.Duration(seconds) * time.Second)
	}
	return time.Time{}
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}
