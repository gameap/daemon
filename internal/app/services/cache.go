package services

import (
	"context"
	"time"

	"github.com/dgraph-io/ristretto"
	"github.com/gameap/daemon/internal/app/config"
)

type LocalCache struct {
	cacheStore *ristretto.Cache
}

func NewLocalCache(_ *config.Config) (*LocalCache, error) {
	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e7,
		MaxCost:     100 << 20, // 100 Mb
		BufferItems: 64,
	})
	if err != nil {
		return nil, err
	}
	return &LocalCache{cacheStore: cache}, nil
}

func (cache *LocalCache) Set(_ context.Context, key string, val interface{}, ttl time.Duration) {
	cache.cacheStore.SetWithTTL(key, val, 1, ttl)
}

func (cache *LocalCache) Get(_ context.Context, key string) {
	cache.cacheStore.Get(key)
}

func (cache *LocalCache) Delete(_ context.Context, key string) {
	cache.cacheStore.Del(key)
}
