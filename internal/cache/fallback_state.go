package cache

import (
	"context"
	"math"
	"sync"
	"time"
)

// Redis 优先、进程内存兜底的短期安全状态：2FA 挑战失败计数与吊销标记、签名请求防重放。
// Redis 未启用时使用进程内存；已启用但调用失败时拒绝认证，避免丢失其他实例的安全状态。
// 进程内存只对当前实例生效，多实例部署仍需配置共享 Redis。

type localStateEntry struct {
	value     int64
	expiresAt time.Time
}

const localStateSweepInterval = time.Minute

var localState = struct {
	sync.Mutex
	entries   map[string]localStateEntry
	lastSweep time.Time
}{entries: map[string]localStateEntry{}}

// IncrChallengeCounter 将计数加一并返回新值，首次计数时设置过期时间。
func IncrChallengeCounter(ctx context.Context, key string, ttl time.Duration) int64 {
	if rdb := Client(); rdb != nil {
		count, err := rdb.Incr(ctx, key).Result()
		if err == nil {
			if count == 1 {
				if err := rdb.Expire(ctx, key, ttl).Err(); err != nil {
					return math.MaxInt64
				}
			}
			return count
		}
		return math.MaxInt64
	}
	return incrLocalState(key, ttl, time.Now())
}

// SetChallengeFlag 写入带过期时间的标记。
func SetChallengeFlag(ctx context.Context, key string, ttl time.Duration) {
	setLocalState(key, ttl, time.Now())
	if rdb := Client(); rdb != nil {
		_ = rdb.Set(ctx, key, "1", ttl).Err()
	}
}

// HasChallengeFlag 判断标记是否存在；Redis 故障期间写入进程内存的标记同样生效。
func HasChallengeFlag(ctx context.Context, key string) bool {
	if localStateExists(key, time.Now()) {
		return true
	}
	if rdb := Client(); rdb != nil {
		exists, err := rdb.Exists(ctx, key).Result()
		return err != nil || exists != 0
	}
	return false
}

// ClaimOnce 原子地占用一次性标记：有效期内首次占用返回 true，再次占用返回 false。
func ClaimOnce(ctx context.Context, key string, ttl time.Duration) bool {
	if localStateExists(key, time.Now()) {
		return false
	}
	if rdb := Client(); rdb != nil {
		claimed, err := rdb.SetNX(ctx, buildKey(key), "1", ttl).Result()
		if err != nil {
			return false
		}
		if claimed {
			setLocalState(key, ttl, time.Now())
		}
		return claimed
	}
	return claimLocalState(key, ttl, time.Now())
}

func incrLocalState(key string, ttl time.Duration, now time.Time) int64 {
	localState.Lock()
	defer localState.Unlock()
	sweepLocalStateLocked(now)
	entry, ok := localState.entries[key]
	if !ok || !now.Before(entry.expiresAt) {
		entry = localStateEntry{expiresAt: now.Add(ttl)}
	}
	entry.value++
	localState.entries[key] = entry
	return entry.value
}

func setLocalState(key string, ttl time.Duration, now time.Time) {
	localState.Lock()
	defer localState.Unlock()
	sweepLocalStateLocked(now)
	localState.entries[key] = localStateEntry{value: 1, expiresAt: now.Add(ttl)}
}

func localStateExists(key string, now time.Time) bool {
	localState.Lock()
	defer localState.Unlock()
	entry, ok := localState.entries[key]
	return ok && now.Before(entry.expiresAt)
}

func claimLocalState(key string, ttl time.Duration, now time.Time) bool {
	localState.Lock()
	defer localState.Unlock()
	sweepLocalStateLocked(now)
	if entry, ok := localState.entries[key]; ok && now.Before(entry.expiresAt) {
		return false
	}
	localState.entries[key] = localStateEntry{value: 1, expiresAt: now.Add(ttl)}
	return true
}

// sweepLocalStateLocked 每分钟最多清理一次过期条目，避免长期运行时内存增长。
func sweepLocalStateLocked(now time.Time) {
	if now.Sub(localState.lastSweep) < localStateSweepInterval {
		return
	}
	localState.lastSweep = now
	for key, entry := range localState.entries {
		if !now.Before(entry.expiresAt) {
			delete(localState.entries, key)
		}
	}
}
