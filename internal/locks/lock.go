package locks

import (
	"context"
	"sync"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

type mutexKV struct {
	mu    sync.Mutex
	store map[string]chan struct{}
}

func (m *mutexKV) Lock(ctx context.Context, key string) error {
	tflog.Debug(ctx, "Locking", map[string]any{"key": key})
	ch := m.getOrCreate(key)
	select {
	case <-ch:
		tflog.Debug(ctx, "Locked", map[string]any{"key": key})
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *mutexKV) Unlock(ctx context.Context, key string) {
	tflog.Debug(ctx, "Unlocking", map[string]any{"key": key})
	ch := m.getOrCreate(key)
	ch <- struct{}{}
	tflog.Debug(ctx, "Unlocked", map[string]any{"key": key})
}

func (m *mutexKV) getOrCreate(key string) chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	ch, ok := m.store[key]
	if !ok {
		ch = make(chan struct{}, 1)
		ch <- struct{}{} // start unlocked
		m.store[key] = ch
	}
	return ch
}

func NewMutexKV() *mutexKV {
	return &mutexKV{
		store: make(map[string]chan struct{}),
	}
}

var monoMutexKV = NewMutexKV()

func Lock(ctx context.Context, key string) error {
	return monoMutexKV.Lock(ctx, key)
}

func Unlock(ctx context.Context, key string) {
	monoMutexKV.Unlock(ctx, key)
}
