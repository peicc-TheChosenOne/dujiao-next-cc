package cache

import (
	"context"
	"testing"
	"time"
)

func TestFallbackStateWorksWithoutRedis(t *testing.T) {
	if Client() != nil {
		t.Skip("redis is enabled in this process")
	}
	ctx := context.Background()
	prefix := "test:" + t.Name() + ":"

	for want := int64(1); want <= 5; want++ {
		if got := IncrChallengeCounter(ctx, prefix+"fails", time.Minute); got != want {
			t.Fatalf("IncrChallengeCounter() = %d, want %d", got, want)
		}
	}
	if HasChallengeFlag(ctx, prefix+"revoked") {
		t.Fatal("flag must not exist before it is set")
	}
	SetChallengeFlag(ctx, prefix+"revoked", time.Minute)
	if !HasChallengeFlag(ctx, prefix+"revoked") {
		t.Fatal("revocation flag set without redis must be visible")
	}
	if !ClaimOnce(ctx, prefix+"claim", time.Minute) {
		t.Fatal("first claim must succeed")
	}
	if ClaimOnce(ctx, prefix+"claim", time.Minute) {
		t.Fatal("second claim within ttl must fail")
	}
}

func TestLocalStateEntriesExpire(t *testing.T) {
	now := time.Now()
	key := "test:" + t.Name()

	if got := incrLocalState(key, time.Minute, now); got != 1 {
		t.Fatalf("first increment = %d, want 1", got)
	}
	if got := incrLocalState(key, time.Minute, now.Add(2*time.Minute)); got != 1 {
		t.Fatalf("increment after expiry = %d, want counter restarted at 1", got)
	}

	setLocalState(key+":flag", time.Minute, now)
	if !localStateExists(key+":flag", now.Add(30*time.Second)) {
		t.Fatal("flag must exist before expiry")
	}
	if localStateExists(key+":flag", now.Add(2*time.Minute)) {
		t.Fatal("flag must disappear after expiry")
	}

	if !claimLocalState(key+":claim", time.Minute, now) {
		t.Fatal("first claim must succeed")
	}
	if !claimLocalState(key+":claim", time.Minute, now.Add(2*time.Minute)) {
		t.Fatal("claim must be reusable after expiry")
	}
}
