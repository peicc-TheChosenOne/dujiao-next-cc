package cache

import (
	"context"
	"errors"
	"fmt"
	"github.com/redis/go-redis/v9"
	"sync"
	"testing"
	"time"
)

type securityStateRedisHook struct {
	values  map[string]int64
	failure bool
}

func (h *securityStateRedisHook) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h *securityStateRedisHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
func (h *securityStateRedisHook) ProcessHook(_ redis.ProcessHook) redis.ProcessHook {
	return func(_ context.Context, cmd redis.Cmder) error {
		if h.failure {
			return errors.New("injected Redis failure")
		}
		key := fmt.Sprint(cmd.Args()[1])
		switch cmd.Name() {
		case "set":
			if c, ok := cmd.(*redis.BoolCmd); ok {
				_, exists := h.values[key]
				c.SetVal(!exists)
				h.values[key] = 1
			} else {
				h.values[key] = 1
				cmd.(*redis.StatusCmd).SetVal("OK")
			}
		case "exists":
			_, exists := h.values[key]
			if exists {
				cmd.(*redis.IntCmd).SetVal(1)
			} else {
				cmd.(*redis.IntCmd).SetVal(0)
			}
		case "incr":
			h.values[key]++
			cmd.(*redis.IntCmd).SetVal(h.values[key])
		case "expire":
			cmd.(*redis.BoolCmd).SetVal(true)
		default:
			return fmt.Errorf("unexpected Redis command %s", cmd.Name())
		}
		return nil
	}
}
func securityStateRedisFixture(t *testing.T) *securityStateRedisHook {
	t.Helper()
	oldEnabled, oldClient := redisEnabled, redisClient
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", MaxRetries: -1})
	t.Cleanup(func() { redisEnabled, redisClient = oldEnabled, oldClient; _ = client.Close() })
	hook := &securityStateRedisHook{values: map[string]int64{}}
	client.AddHook(hook)
	redisEnabled, redisClient = true, client
	return hook
}
func TestSecurityStateRejectsReplayAcrossRedisFailure(t *testing.T) {
	hook := securityStateRedisFixture(t)
	ctx := context.Background()
	key := "test:" + t.Name()
	if !ClaimOnce(ctx, key, time.Minute) {
		t.Fatal("first claim failed")
	}
	hook.failure = true
	if ClaimOnce(ctx, key, time.Minute) {
		t.Fatal("Redis outage must not accept a previously used signature")
	}
	if ClaimOnce(ctx, key+":new", time.Minute) {
		t.Fatal("ambiguous Redis state must reject new signed writes")
	}
	hook.failure = false
	if ClaimOnce(ctx, key, time.Minute) {
		t.Fatal("recovery must preserve prior claims")
	}
	if !ClaimOnce(ctx, key+":new", time.Minute) {
		t.Fatal("a rejected, never-used signature may succeed after recovery")
	}
	redisEnabled = false
	if ClaimOnce(ctx, key, time.Minute) {
		t.Fatal("local mirror must preserve Redis claims")
	}
}
func TestSecurityStateRejects2FAWhenRedisFails(t *testing.T) {
	hook := securityStateRedisFixture(t)
	ctx := context.Background()
	key := "test:" + t.Name()
	if HasChallengeFlag(ctx, key) {
		t.Fatal("healthy unused challenge must be allowed")
	}
	SetChallengeFlag(ctx, key, time.Minute)
	hook.failure = true
	if !HasChallengeFlag(ctx, key) {
		t.Fatal("revocation must survive outage")
	}
	if !HasChallengeFlag(ctx, key+":other-instance") {
		t.Fatal("unknown shared revocation state must fail closed")
	}
	if IncrChallengeCounter(ctx, key+":fails", time.Minute) < 5 {
		t.Fatal("unknown attempt counter must fail closed")
	}
	SetChallengeFlag(ctx, key+":during-outage", time.Minute)
	hook.failure = false
	if !HasChallengeFlag(ctx, key+":during-outage") {
		t.Fatal("local revocation must survive recovery")
	}
}
func TestLocalSecurityStateAllowsOnlyOneConcurrentClaim(t *testing.T) {
	oldEnabled := redisEnabled
	redisEnabled = false
	t.Cleanup(func() { redisEnabled = oldEnabled })
	results := make(chan bool, 40)
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- ClaimOnce(context.Background(), "test:"+t.Name(), time.Minute) }()
	}
	wg.Wait()
	close(results)
	accepted := 0
	for ok := range results {
		if ok {
			accepted++
		}
	}
	if accepted != 1 {
		t.Fatalf("accepted %d concurrent claims", accepted)
	}
}

func TestChallengeConsumptionRejectsRedisWriteFailures(t *testing.T) {
	hook := securityStateRedisFixture(t)
	hook.failure = true
	if SetChallengeFlag(context.Background(), "test:"+t.Name(), time.Minute) {
		t.Fatal("shared-state failure must not allow login")
	}
	hook.failure = false
	if SetChallengeFlag(context.Background(), "test:"+t.Name(), time.Minute) {
		t.Fatal("locally consumed challenge must not be accepted again")
	}
}
