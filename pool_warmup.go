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

// PoolWarmer 内存池预热器
// 在应用启动时预先分配和填充内存池，减少运行时分配开销
type PoolWarmer struct {
	bytePool     *SizedBytePool
	textPool     *SizedTextSlicePool
	warmed       atomic.Value // bool
	warmingMutex sync.Mutex
}

// GlobalPoolWarmer 全局池预热器实例
var GlobalPoolWarmer = &PoolWarmer{
	bytePool: globalSizedBytePool,
	textPool: globalSizedTextSlicePool,
}

func init() {
	GlobalPoolWarmer.warmed.Store(false)
}

// WarmupConfig 预热配置
type WarmupConfig struct {
	// BytePoolWarmup 每个大小桶预热的缓冲区数量
	BytePoolWarmup map[int]int

	// TextPoolWarmup 每个大小桶预热的文本切片数量
	TextPoolWarmup map[int]int

	// Concurrent 是否并发预热
	Concurrent bool

	// MaxGoroutines 最大并发 goroutine 数
	MaxGoroutines int
}

// DefaultWarmupConfig 返回默认预热配置
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

// AggressiveWarmupConfig 返回激进的预热配置（更多预分配）
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

// LightWarmupConfig 返回轻量预热配置（较少预分配）
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

// Warmup 执行内存池预热
func (pw *PoolWarmer) Warmup(config *WarmupConfig) error {
	pw.warmingMutex.Lock()
	defer pw.warmingMutex.Unlock()

	if pw.IsWarmed() {
		return nil // 已经预热过了
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

// warmupSequential 顺序预热
func (pw *PoolWarmer) warmupSequential(config *WarmupConfig) {
	// 预热字节池
	for size, count := range config.BytePoolWarmup {
		buffers := make([][]byte, count)
		for i := 0; i < count; i++ {
			buffers[i] = pw.bytePool.Get(size)
		}
		// 返回到池中
		for _, buf := range buffers {
			pw.bytePool.Put(buf)
		}
	}

	// 预热文本切片池
	for size, count := range config.TextPoolWarmup {
		slices := make([][]Text, count)
		for i := 0; i < count; i++ {
			slices[i] = pw.textPool.Get(size)
		}
		// 返回到池中
		for _, slice := range slices {
			pw.textPool.Put(slice)
		}
	}
}

// warmupConcurrent 并发预热
func (pw *PoolWarmer) warmupConcurrent(config *WarmupConfig) {
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, config.MaxGoroutines)

	// 并发预热字节池
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

	// 并发预热文本切片池
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

// IsWarmed 检查是否已预热
func (pw *PoolWarmer) IsWarmed() bool {
	warmed, ok := pw.warmed.Load().(bool)
	return ok && warmed
}

// Reset 重置预热状态
func (pw *PoolWarmer) Reset() {
	pw.warmingMutex.Lock()
	defer pw.warmingMutex.Unlock()
	pw.warmed.Store(false)
}

// WarmupGlobal 预热全局内存池（便捷函数）
func WarmupGlobal(config *WarmupConfig) error {
	return GlobalPoolWarmer.Warmup(config)
}

// AutoWarmup 自动预热（根据可用内存选择配置）
func AutoWarmup() error {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	// 根据可用内存选择配置
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

// PreallocateCache 预分配缓存（附加功能）
func PreallocateCache(fontCacheSize, resultCacheSize int) {
	if fontCacheSize > 0 {
		_ = NewOptimizedFontCache(fontCacheSize)
	}
	if resultCacheSize > 0 {
		_ = NewShardedCache(resultCacheSize, 0)
	}
}

// WarmupStats 预热统计信息
type WarmupStats struct {
	BytePoolSizes  map[int]int
	TextPoolSizes  map[int]int
	TotalAllocated int64
	IsWarmed       bool
}

// GetWarmupStats 获取预热统计信息
func (pw *PoolWarmer) GetWarmupStats() WarmupStats {
	return WarmupStats{
		IsWarmed: pw.IsWarmed(),
	}
}

// OptimizedStartup 优化启动流程
// 包括池预热、缓存预分配等
func OptimizedStartup(config *StartupConfig) error {
	if config == nil {
		config = DefaultStartupConfig()
	}

	// 1. 预热内存池
	if config.WarmupPools {
		if err := GlobalPoolWarmer.Warmup(config.WarmupConfig); err != nil {
			return err
		}
	}

	// 2. 预分配缓存
	if config.PreallocateCaches {
		PreallocateCache(config.FontCacheSize, config.ResultCacheSize)
	}

	// 3. 调整 GC 参数
	if config.TuneGC {
		// 设置 GC 目标百分比（默认 100）
		// 更高的值会减少 GC 频率但增加内存使用
		if config.GCPercent > 0 {
			debug.SetGCPercent(config.GCPercent)
		}

		// 预留内存以减少 GC 压力
		if config.MemoryBallast > 0 {
			_ = make([]byte, config.MemoryBallast)
		}
	}

	// 4. 设置 GOMAXPROCS
	if config.SetMaxProcs {
		if config.MaxProcs <= 0 {
			config.MaxProcs = runtime.NumCPU()
		}
		runtime.GOMAXPROCS(config.MaxProcs)
	}

	return nil
}

// StartupConfig 启动配置
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

// DefaultStartupConfig 默认启动配置
func DefaultStartupConfig() *StartupConfig {
	return &StartupConfig{
		WarmupPools:       true,
		WarmupConfig:      DefaultWarmupConfig(),
		PreallocateCaches: true,
		FontCacheSize:     1000,
		ResultCacheSize:   10000,
		TuneGC:            true,
		GCPercent:         200,              // 减少 GC 频率
		MemoryBallast:     10 * 1024 * 1024, // 10MB ballast
		SetMaxProcs:       true,
		MaxProcs:          0, // 自动检测
	}
}
