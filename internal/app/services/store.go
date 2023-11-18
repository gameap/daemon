package services

import (
	"context"
	"sync"

	"github.com/gameap/daemon/internal/app/config"
)

type LocalStore struct {
	items sync.Map
}

func NewLocalStore(_ *config.Config) (*LocalStore, error) {
	return &LocalStore{}, nil
}

func (store *LocalStore) Set(_ context.Context, key string, val interface{}) {
	store.items.Store(key, val)
}

func (store *LocalStore) Get(_ context.Context, key string) {
	store.items.Load(key)
}

func (store *LocalStore) Delete(_ context.Context, key string) {
	store.items.Delete(key)
}
