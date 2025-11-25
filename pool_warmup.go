// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
)

// PoolWarmer memory pool warmer
// Pre-allocate and fill memory pools at application startup to reduce runtime allocation overhead
type PoolWarmer struct {
	bytePool     *SizedBytePool
	textPool     *SizedTextSlicePool
	warmed       atomic.Value // bool
	warmingMutex sync.Mutex
}

// GlobalPoolWarmer global pool warmer instance
var GlobalPoolWarmer = &PoolWarmer{
	bytePool: globalSizedBytePool,
	textPool: globalSizedTextSlicePool,
}

func init() {
	GlobalPoolWarmer.warmed.Store(false)
}

// WarmupConfig warmup configuration
type WarmupConfig struct {
	// BytePoolWarmup number of buffers to warmup for each size bucket
	BytePoolWarmup map[int]int

	// TextPoolWarmup number of text slices to warmup for each size bucket
	TextPoolWarmup map[int]int

	// Concurrent whether to warmup concurrently
	Concurrent bool

	// MaxGoroutines maximum number of concurrent goroutines
	MaxGoroutines int
}

// DefaultWarmupConfig returns default warmup configuration
func DefaultWarmupConfig() *WarmupConfig {
	return &WarmupConfig{
		BytePoolWarmup: map[int]int{
			16:   100,
			32:   100,
			64:   80,
			128:  60,
			256:  40,
			512:  30,
			1024: 20,
			4096: 10,
		},
		TextPoolWarmup: map[int]int{
			8:   50,
			16:  40,
			32:  30,
			64:  20,
			128: 10,
			256: 5,
		},
		Concurrent:    true,
		MaxGoroutines: runtime.NumCPU(),
	}
}

// AggressiveWarmupConfig returns aggressive warmup configuration (more pre-allocation)
func AggressiveWarmupConfig() *WarmupConfig {
	return &WarmupConfig{
		BytePoolWarmup: map[int]int{
			16:   500,
			32:   500,
			64:   400,
			128:  300,
			256:  200,
			512:  150,
			1024: 100,
			4096: 50,
		},
		TextPoolWarmup: map[int]int{
			8:   200,
			16:  150,
			32:  100,
			64:  75,
			128: 50,
			256: 25,
		},
		Concurrent:    true,
		MaxGoroutines: runtime.NumCPU() * 2,
	}
}

// LightWarmupConfig returns light warmup configuration (less pre-allocation)
func LightWarmupConfig() *WarmupConfig {
	return &WarmupConfig{
		BytePoolWarmup: map[int]int{
			16:   20,
			32:   20,
			64:   15,
			128:  10,
			256:  8,
			512:  5,
			1024: 3,
			4096: 2,
		},
		TextPoolWarmup: map[int]int{
			8:   10,
			16:  8,
			32:  6,
			64:  4,
			128: 2,
			256: 1,
		},
		Concurrent:    false,
		MaxGoroutines: 1,
	}
}

// Warmup performs memory pool warmup
func (pw *PoolWarmer) Warmup(config *WarmupConfig) error {
	pw.warmingMutex.Lock()
	defer pw.warmingMutex.Unlock()

	if pw.IsWarmed() {
		return nil // already warmed up
	}

	if config == nil {
		config = DefaultWarmupConfig()
	}

	if config.Concurrent {
		pw.warmupConcurrent(config)
	} else {
		pw.warmupSequential(config)
	}

	pw.warmed.Store(true)
	return nil
}

// warmupSequential sequential warmup
func (pw *PoolWarmer) warmupSequential(config *WarmupConfig) {
	// warmup byte pool
	for size, count := range config.BytePoolWarmup {
		buffers := make([][]byte, count)
		for i := 0; i < count; i++ {
			buffers[i] = pw.bytePool.Get(size)
		}
		// return to pool
		for _, buf := range buffers {
			pw.bytePool.Put(buf)
		}
	}

	// warmup text slice pool
	for size, count := range config.TextPoolWarmup {
		slices := make([][]Text, count)
		for i := 0; i < count; i++ {
			slices[i] = pw.textPool.Get(size)
		}
		// return to pool
		for _, slice := range slices {
			pw.textPool.Put(slice)
		}
	}
}

// warmupConcurrent concurrent warmup
func (pw *PoolWarmer) warmupConcurrent(config *WarmupConfig) {
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, config.MaxGoroutines)

	// concurrent warmup byte pool
	for size, count := range config.BytePoolWarmup {
		wg.Add(1)
		semaphore <- struct{}{}

		go func(sz, cnt int) {
			defer wg.Done()
			defer func() { <-semaphore }()

			buffers := make([][]byte, cnt)
			for i := 0; i < cnt; i++ {
				buffers[i] = pw.bytePool.Get(sz)
			}
			for _, buf := range buffers {
				pw.bytePool.Put(buf)
			}
		}(size, count)
	}

	// concurrent warmup text slice pool
	for size, count := range config.TextPoolWarmup {
		wg.Add(1)
		semaphore <- struct{}{}

		go func(sz, cnt int) {
			defer wg.Done()
			defer func() { <-semaphore }()

			slices := make([][]Text, cnt)
			for i := 0; i < cnt; i++ {
				slices[i] = pw.textPool.Get(sz)
			}
			for _, slice := range slices {
				pw.textPool.Put(slice)
			}
		}(size, count)
	}

	wg.Wait()
}

// IsWarmed checks if warmed up
func (pw *PoolWarmer) IsWarmed() bool {
	warmed, ok := pw.warmed.Load().(bool)
	return ok && warmed
}

// Reset resets warmup state
func (pw *PoolWarmer) Reset() {
	pw.warmingMutex.Lock()
	defer pw.warmingMutex.Unlock()
	pw.warmed.Store(false)
}

// WarmupGlobal warms up global memory pool (convenience function)
func WarmupGlobal(config *WarmupConfig) error {
	return GlobalPoolWarmer.Warmup(config)
}

// AutoWarmup automatic warmup (selects config based on available memory)
func AutoWarmup() error {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	// select config based on available memory
	var config *WarmupConfig
	if ms.Sys > 1024*1024*1024 { // > 1GB
		config = AggressiveWarmupConfig()
	} else if ms.Sys > 256*1024*1024 { // > 256MB
		config = DefaultWarmupConfig()
	} else {
		config = LightWarmupConfig()
	}

	return GlobalPoolWarmer.Warmup(config)
}

// PreallocateCache pre-allocates cache (additional feature)
func PreallocateCache(fontCacheSize, resultCacheSize int) {
	if fontCacheSize > 0 {
		_ = NewOptimizedFontCache(fontCacheSize)
	}
	if resultCacheSize > 0 {
		_ = NewShardedCache(resultCacheSize, 0)
	}
}

// WarmupStats warmup statistics
type WarmupStats struct {
	BytePoolSizes  map[int]int
	TextPoolSizes  map[int]int
	TotalAllocated int64
	IsWarmed       bool
}

// GetWarmupStats gets warmup statistics
func (pw *PoolWarmer) GetWarmupStats() WarmupStats {
	return WarmupStats{
		IsWarmed: pw.IsWarmed(),
	}
}

// OptimizedStartup optimized startup process
// includes pool warmup, cache pre-allocation, etc.
func OptimizedStartup(config *StartupConfig) error {
	if config == nil {
		config = DefaultStartupConfig()
	}

	// 1. warmup memory pools
	if config.WarmupPools {
		if err := GlobalPoolWarmer.Warmup(config.WarmupConfig); err != nil {
			return err
		}
	}

	// 2. pre-allocate caches
	if config.PreallocateCaches {
		PreallocateCache(config.FontCacheSize, config.ResultCacheSize)
	}

	// 3. adjust GC parameters
	if config.TuneGC {
		// set GC target percentage (default 100)
		// higher values reduce GC frequency but increase memory usage
		if config.GCPercent > 0 {
			debug.SetGCPercent(config.GCPercent)
		}

		// reserve memory to reduce GC pressure
		if config.MemoryBallast > 0 {
			_ = make([]byte, config.MemoryBallast)
		}
	}

	// 4. set GOMAXPROCS
	if config.SetMaxProcs {
		if config.MaxProcs <= 0 {
			config.MaxProcs = runtime.NumCPU()
		}
		runtime.GOMAXPROCS(config.MaxProcs)
	}

	return nil
}

// StartupConfig startup configuration
type StartupConfig struct {
	WarmupPools       bool
	WarmupConfig      *WarmupConfig
	PreallocateCaches bool
	FontCacheSize     int
	ResultCacheSize   int
	TuneGC            bool
	GCPercent         int
	MemoryBallast     int64
	SetMaxProcs       bool
	MaxProcs          int
}

// DefaultStartupConfig default startup configuration
func DefaultStartupConfig() *StartupConfig {
	return &StartupConfig{
		WarmupPools:       true,
		WarmupConfig:      DefaultWarmupConfig(),
		PreallocateCaches: true,
		FontCacheSize:     1000,
		ResultCacheSize:   10000,
		TuneGC:            true,
		GCPercent:         200,              // reduce GC frequency
		MemoryBallast:     10 * 1024 * 1024, // 10MB ballast
		SetMaxProcs:       true,
		MaxProcs:          0, // auto-detect
	}
}
