package storage

import "time"

// EvictionSampleSize 采样淘汰时随机抽样的条数
const EvictionSampleSize = 16

// EvictOldestSampled 以近似 LRU 的方式从 m 中淘汰一条“最旧”记录：
// 随机采样 sample 条，删除其中 lastSeen 最小的一条并返回是否删除成功。
func EvictOldestSampled[K comparable, V any](m map[K]V, lastSeen func(V) time.Time, sample int) bool {
	if len(m) == 0 {
		return false
	}
	if sample > len(m) {
		sample = len(m)
	}
	var (
		oldestKey  K
		oldestAt   time.Time
		haveOldest bool
	)
	i := 0
	for key, value := range m {
		at := lastSeen(value)
		if !haveOldest || at.Before(oldestAt) {
			oldestKey = key
			oldestAt = at
			haveOldest = true
		}
		i++
		if i >= sample {
			break
		}
	}
	if !haveOldest {
		return false
	}
	delete(m, oldestKey)
	return true
}
