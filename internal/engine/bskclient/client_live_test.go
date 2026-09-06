package bskclient

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestLiveDaemonStatus runs against the real bsk daemon when one is up;
// read-only, so it is safe to run anywhere (skips otherwise).
func TestLiveDaemonStatus(t *testing.T) {
	c := New()
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	st, err := c.Status(ctx)
	if err != nil {
		t.Skipf("no live bsk daemon: %v", err)
	}
	t.Logf("daemon %s protocol %s pid %d, %d browser(s) connected",
		st.DaemonVersion, st.ProtocolVersion, st.PID, len(st.Browsers))
	if st.ProtocolVersion != SupportedProtocol {
		t.Errorf("protocol drift: client speaks %s, daemon speaks %s", SupportedProtocol, st.ProtocolVersion)
	}
}

// TestLiveRoundTrip exercises a real evaluate round-trip against the live
// daemon. Gated: it opens a tab in the user's Agent Window.
func TestLiveRoundTrip(t *testing.T) {
	if os.Getenv("BSK_LIVE_TEST") != "1" {
		t.Skip("set BSK_LIVE_TEST=1 to run against the real daemon/browser")
	}
	c := New()
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	sess, err := c.SessionStart(ctx, true)
	if err != nil {
		t.Fatalf("session start: %v", err)
	}
	defer c.SessionStop(context.Background(), sess.SessionID)

	tabID, err := c.TabCreate(ctx, sess.SessionID, "https://example.com")
	if err != nil {
		t.Fatalf("tab create: %v", err)
	}
	// Give the page a moment to commit.
	time.Sleep(2 * time.Second)

	got, err := c.EvaluateString(ctx, sess.SessionID, tabID, "document.title", 10*time.Second)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	t.Logf("example.com title: %q", got)
	if got == "" {
		t.Error("expected non-empty title")
	}
}
