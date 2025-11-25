// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"runtime"
	"testing"
)

func TestPoolWarmerBasic(t *testing.T) {
	warmer := &PoolWarmer{
		bytePool: NewSizedBytePool(),
		textPool: NewSizedTextSlicePool(),
	}
	warmer.warmed.Store(false)

	if warmer.IsWarmed() {
		t.Error("Pool should not be warmed initially")
	}

	err := warmer.Warmup(LightWarmupConfig())
	if err != nil {
		t.Fatalf("Warmup failed: %v", err)
	}

	if !warmer.IsWarmed() {
		t.Error("Pool should be warmed after warmup")
	}

	// Second warmup should not error
	err = warmer.Warmup(nil)
	if err != nil {
		t.Fatalf("Second warmup failed: %v", err)
	}
}

func TestWarmupConfigs(t *testing.T) {
	tests := []struct {
		name   string
		config *WarmupConfig
	}{
		{"Default", DefaultWarmupConfig()},
		{"Aggressive", AggressiveWarmupConfig()},
		{"Light", LightWarmupConfig()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warmer := &PoolWarmer{
				bytePool: NewSizedBytePool(),
				textPool: NewSizedTextSlicePool(),
			}
			warmer.warmed.Store(false)

			err := warmer.Warmup(tt.config)
			if err != nil {
				t.Fatalf("Warmup with %s config failed: %v", tt.name, err)
			}

			if !warmer.IsWarmed() {
				t.Errorf("%s warmup didn't mark as warmed", tt.name)
			}
		})
	}
}

func TestWarmupSequentialVsConcurrent(t *testing.T) {
	t.Run("Sequential", func(t *testing.T) {
		config := LightWarmupConfig()
		config.Concurrent = false

		warmer := &PoolWarmer{
			bytePool: NewSizedBytePool(),
			textPool: NewSizedTextSlicePool(),
		}
		warmer.warmed.Store(false)

		err := warmer.Warmup(config)
		if err != nil {
			t.Fatalf("Sequential warmup failed: %v", err)
		}
	})

	t.Run("Concurrent", func(t *testing.T) {
		config := LightWarmupConfig()
		config.Concurrent = true
		config.MaxGoroutines = 4

		warmer := &PoolWarmer{
			bytePool: NewSizedBytePool(),
			textPool: NewSizedTextSlicePool(),
		}
		warmer.warmed.Store(false)

		err := warmer.Warmup(config)
		if err != nil {
			t.Fatalf("Concurrent warmup failed: %v", err)
		}
	})
}

func TestAutoWarmup(t *testing.T) {
	// Reset global warmer
	GlobalPoolWarmer.Reset()

	err := AutoWarmup()
	if err != nil {
		t.Fatalf("AutoWarmup failed: %v", err)
	}

	if !GlobalPoolWarmer.IsWarmed() {
		t.Error("GlobalPoolWarmer should be warmed after AutoWarmup")
	}
}

func TestOptimizedStartup(t *testing.T) {
	config := &StartupConfig{
		WarmupPools:       true,
		WarmupConfig:      LightWarmupConfig(),
		PreallocateCaches: true,
		FontCacheSize:     100,
		ResultCacheSize:   1000,
		TuneGC:            false, // do not adjust GC to avoid affecting other tests
		SetMaxProcs:       false,
	}

	err := OptimizedStartup(config)
	if err != nil {
		t.Fatalf("OptimizedStartup failed: %v", err)
	}
}

func TestPreallocateCache(t *testing.T) {
	// This mainly tests that it doesn't crash
	PreallocateCache(100, 1000)
	PreallocateCache(0, 0)
}

func TestWarmupReset(t *testing.T) {
	warmer := &PoolWarmer{
		bytePool: NewSizedBytePool(),
		textPool: NewSizedTextSlicePool(),
	}
	warmer.warmed.Store(false)

	err := warmer.Warmup(LightWarmupConfig())
	if err != nil {
		t.Fatalf("Warmup failed: %v", err)
	}

	if !warmer.IsWarmed() {
		t.Error("Should be warmed")
	}

	warmer.Reset()

	if warmer.IsWarmed() {
		t.Error("Should not be warmed after reset")
	}
}

func BenchmarkWarmup(b *testing.B) {
	configs := map[string]*WarmupConfig{
		"Light":      LightWarmupConfig(),
		"Default":    DefaultWarmupConfig(),
		"Aggressive": AggressiveWarmupConfig(),
	}

	for name, config := range configs {
		b.Run(name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				warmer := &PoolWarmer{
					bytePool: NewSizedBytePool(),
					textPool: NewSizedTextSlicePool(),
				}
				warmer.warmed.Store(false)
				warmer.Warmup(config)
			}
		})
	}
}

func BenchmarkWarmupSequentialVsConcurrent(b *testing.B) {
	b.Run("Sequential", func(b *testing.B) {
		config := DefaultWarmupConfig()
		config.Concurrent = false

		for i := 0; i < b.N; i++ {
			warmer := &PoolWarmer{
				bytePool: NewSizedBytePool(),
				textPool: NewSizedTextSlicePool(),
			}
			warmer.warmed.Store(false)
			warmer.Warmup(config)
		}
	})

	b.Run("Concurrent", func(b *testing.B) {
		config := DefaultWarmupConfig()
		config.Concurrent = true
		config.MaxGoroutines = runtime.NumCPU()

		for i := 0; i < b.N; i++ {
			warmer := &PoolWarmer{
				bytePool: NewSizedBytePool(),
				textPool: NewSizedTextSlicePool(),
			}
			warmer.warmed.Store(false)
			warmer.Warmup(config)
		}
	})
}

func BenchmarkPoolGetAfterWarmup(b *testing.B) {
	warmer := &PoolWarmer{
		bytePool: NewSizedBytePool(),
		textPool: NewSizedTextSlicePool(),
	}
	warmer.warmed.Store(false)

	b.Run("NoWarmup", func(b *testing.B) {
		pool := NewSizedBytePool()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			buf := pool.Get(128)
			pool.Put(buf)
		}
	})

	b.Run("WithWarmup", func(b *testing.B) {
		pool := NewSizedBytePool()
		tempWarmer := &PoolWarmer{
			bytePool: pool,
			textPool: NewSizedTextSlicePool(),
		}
		tempWarmer.warmed.Store(false)
		tempWarmer.Warmup(DefaultWarmupConfig())

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			buf := pool.Get(128)
			pool.Put(buf)
		}
	})
}
