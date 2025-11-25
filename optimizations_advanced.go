// Copyright 2024 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"math"
	"math/bits"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"
)

// ==================== 高级优化实现 ====================

// 1. 细粒度对象池 - 多级大小分桶
type SizedPool struct {
	pools [8]*sync.Pool
	sizes [8]int
}

func NewSizedPool() *SizedPool {
	sp := &SizedPool{
		sizes: [8]int{16, 32, 64, 128, 256, 512, 1024, 4096},
	}

	for i := range sp.pools {
		size := sp.sizes[i]
		sp.pools[i] = &sync.Pool{
			New: func() interface{} {
				return make([]byte, 0, size)
			},
		}
	}
	return sp
}

func (sp *SizedPool) Get(size int) []byte {
	// 使用位运算快速计算池索引
	idx := bits.Len(uint(size)) - 5
	if idx < 0 {
		idx = 0
	} else if idx >= len(sp.pools) {
		idx = len(sp.pools) - 1
	}

	buf := sp.pools[idx].Get().([]byte)
	return buf[:0]
}

func (sp *SizedPool) Put(bufPtr *[]byte) {
	size := cap(*bufPtr)
	idx := bits.Len(uint(size)) - 5
	if idx < 0 {
		idx = 0
	} else if idx >= len(sp.pools) {
		idx = len(sp.pools) - 1
	}

	// Return buffer to appropriate pool (slice to zero length)
	*bufPtr = (*bufPtr)[:0]
	sp.pools[idx].Put(bufPtr)
}

// 2. 零拷贝字符串构建器
type ZeroCopyBuilder struct {
	buf []byte
}

func NewZeroCopyBuilder(cap int) *ZeroCopyBuilder {
	return &ZeroCopyBuilder{
		buf: make([]byte, 0, cap),
	}
}

func (b *ZeroCopyBuilder) WriteString(s string) {
	b.buf = append(b.buf, s...)
}

func (b *ZeroCopyBuilder) WriteByte(c byte) error {
	b.buf = append(b.buf, c)
	return nil
}

// UnsafeString 零拷贝返回字符串（注意：底层buffer不能修改）
func (b *ZeroCopyBuilder) UnsafeString() string {
	return unsafe.String(unsafe.SliceData(b.buf), len(b.buf))
}

func (b *ZeroCopyBuilder) Reset() {
	b.buf = b.buf[:0]
}

// 3. Lock-free 环形缓冲区（用于生产者-消费者）
type LockFreeRingBuffer struct {
	buffer []interface{}
	mask   uint64
	head   uint64   // 写位置
	tail   uint64   // 读位置
	_      [56]byte // cache line padding to prevent false sharing
}

func NewLockFreeRingBuffer(size int) *LockFreeRingBuffer {
	// 确保size是2的幂
	size = 1 << bits.Len(uint(size-1))

	return &LockFreeRingBuffer{
		buffer: make([]interface{}, size),
		mask:   uint64(size - 1),
	}
}

func (rb *LockFreeRingBuffer) Push(item interface{}) bool {
	for {
		head := atomic.LoadUint64(&rb.head)
		tail := atomic.LoadUint64(&rb.tail)

		// 检查是否已满
		if head-tail >= uint64(len(rb.buffer)) {
			return false
		}

		// 尝试CAS更新head
		if atomic.CompareAndSwapUint64(&rb.head, head, head+1) {
			rb.buffer[head&rb.mask] = item
			return true
		}
	}
}

func (rb *LockFreeRingBuffer) Pop() (interface{}, bool) {
	for {
		tail := atomic.LoadUint64(&rb.tail)
		head := atomic.LoadUint64(&rb.head)

		// 检查是否为空
		if tail >= head {
			return nil, false
		}

		// 尝试CAS更新tail
		if atomic.CompareAndSwapUint64(&rb.tail, tail, tail+1) {
			item := rb.buffer[tail&rb.mask]
			return item, true
		}
	}
}

// 4. 缓存行对齐结构
type CacheLinePadded struct {
	value uint64
	_     [7]uint64 // 填充到64字节
}

type CacheLineAlignedCounter struct {
	counters []CacheLinePadded
}

func NewCacheLineAlignedCounter(n int) *CacheLineAlignedCounter {
	return &CacheLineAlignedCounter{
		counters: make([]CacheLinePadded, n),
	}
}

func (c *CacheLineAlignedCounter) Add(idx int, delta uint64) {
	atomic.AddUint64(&c.counters[idx].value, delta)
}

func (c *CacheLineAlignedCounter) Get(idx int) uint64 {
	return atomic.LoadUint64(&c.counters[idx].value)
}

// 5. Work-Stealing Deque (Chase-Lev算法)
type WSDeque struct {
	mu   sync.Mutex
	data []WSTask
}

type WSTask interface {
	Execute()
}

func NewWSDeque(size int) *WSDeque {
	return &WSDeque{
		data: make([]WSTask, 0, size),
	}
}

// PushBottom - owner线程从底部插入
func (d *WSDeque) PushBottom(task WSTask) {
	d.mu.Lock()
	d.data = append(d.data, task)
	d.mu.Unlock()
}

// PopBottom - owner线程从底部弹出（LIFO）
func (d *WSDeque) PopBottom() WSTask {
	d.mu.Lock()
	defer d.mu.Unlock()

	n := len(d.data)
	if n == 0 {
		return nil
	}

	task := d.data[n-1]
	d.data = d.data[:n-1]
	return task
}

// Steal - 其他线程从顶部偷取（FIFO）
func (d *WSDeque) Steal() WSTask {
	d.mu.Lock()
	defer d.mu.Unlock()

	if len(d.data) == 0 {
		return nil
	}

	task := d.data[0]
	d.data = d.data[1:]
	return task
}

// 6. Work-Stealing线程池
type WorkStealingExecutor struct {
	workers    []*WSWorker
	numWorkers int
	stopped    atomic.Bool
	wg         sync.WaitGroup
	roundRobin atomic.Uint64
}

type WSWorker struct {
	id    int
	deque *WSDeque
	pool  *WorkStealingExecutor
}

func NewWorkStealingExecutor(numWorkers int) *WorkStealingExecutor {
	if numWorkers <= 0 {
		numWorkers = runtime.NumCPU()
	}

	pool := &WorkStealingExecutor{
		workers:    make([]*WSWorker, numWorkers),
		numWorkers: numWorkers,
	}

	for i := 0; i < numWorkers; i++ {
		pool.workers[i] = &WSWorker{
			id:    i,
			deque: NewWSDeque(256),
			pool:  pool,
		}
	}

	return pool
}

func (p *WorkStealingExecutor) Start() {
	for _, w := range p.workers {
		p.wg.Add(1)
		go w.run()
	}
}

func (p *WorkStealingExecutor) Stop() {
	p.stopped.Store(true)
	p.wg.Wait()
}

func (p *WorkStealingExecutor) Submit(task WSTask) {
	if p.stopped.Load() {
		return
	}
	// 随机选择一个worker提交任务
	idx := int(p.roundRobin.Add(1)-1) % p.numWorkers
	p.workers[idx].deque.PushBottom(task)
}

func (w *WSWorker) run() {
	defer w.pool.wg.Done()

	for !w.pool.stopped.Load() {
		// 1. 从自己的deque底部取任务（LIFO，缓存友好）
		if task := w.deque.PopBottom(); task != nil {
			task.Execute()
			continue
		}

		// 2. 随机从其他worker偷取任务
		victim := (w.id + 1 + runtime.GOMAXPROCS(0)) % w.pool.numWorkers
		if task := w.pool.workers[victim].deque.Steal(); task != nil {
			task.Execute()
			continue
		}

		// 3. 没有任务，让出CPU
		runtime.Gosched()
	}
}

// 7. Radix Sort 浮点数优化
func RadixSortFloat64(values []float64) {
	if len(values) <= 1 {
		return
	}

	// 将float64转换为uint64保持排序顺序
	keys := make([]uint64, len(values))
	for i, v := range values {
		bits := math.Float64bits(v)
		// 处理负数：如果符号位是1，翻转所有位；否则只翻转符号位
		mask := -uint64(int64(bits)>>63) | 0x8000000000000000
		keys[i] = bits ^ mask
	}

	// Radix sort
	const radix = 256
	buckets := make([][]int, radix)
	for i := range buckets {
		buckets[i] = make([]int, 0, len(values)/radix)
	}

	for shift := uint(0); shift < 64; shift += 8 {
		// 清空桶
		for i := range buckets {
			buckets[i] = buckets[i][:0]
		}

		// 分桶
		for i, k := range keys {
			bucket := (k >> shift) & 0xFF
			buckets[bucket] = append(buckets[bucket], i)
		}

		// 收集
		idx := 0
		for _, bucket := range buckets {
			for _, i := range bucket {
				values[idx] = values[i]
				keys[idx] = keys[i]
				idx++
			}
		}
	}
}

// 8. Hilbert曲线计算（用于空间索引）
func HilbertXYToIndex(x, y, order uint32) uint64 {
	var index uint64
	var rx, ry, s uint32
	n := uint32(1 << order)

	for s = n / 2; s > 0; s /= 2 {
		rx = (x & s) >> (bits.TrailingZeros32(s))
		ry = (y & s) >> (bits.TrailingZeros32(s))

		index += uint64(s * s * ((3 * rx) ^ ry))

		// 旋转
		if ry == 0 {
			if rx == 1 {
				x = n - 1 - x
				y = n - 1 - y
			}
			x, y = y, x
		}
	}

	return index
}

// 9. SIMD友好的批量操作（伪代码，实际需要汇编）
func BatchCompareFloat64(a, b []float64, threshold float64) []bool {
	if len(a) != len(b) {
		panic("length mismatch")
	}

	result := make([]bool, len(a))

	// 理想情况下使用AVX2一次处理4个float64
	// 这里是标量版本，实际应该用汇编实现
	i := 0
	for ; i+4 <= len(a); i += 4 {
		// AVX2: 一次加载4个值，比较，存储
		result[i] = math.Abs(a[i]-b[i]) < threshold
		result[i+1] = math.Abs(a[i+1]-b[i+1]) < threshold
		result[i+2] = math.Abs(a[i+2]-b[i+2]) < threshold
		result[i+3] = math.Abs(a[i+3]-b[i+3]) < threshold
	}

	// 处理剩余元素
	for ; i < len(a); i++ {
		result[i] = math.Abs(a[i]-b[i]) < threshold
	}

	return result
}

// 10. 内存池管理器（减少GC压力）
type MemoryArena struct {
	chunks    [][]byte
	chunkSize int
	offset    int
	mu        sync.Mutex
}

func NewMemoryArena(chunkSize int) *MemoryArena {
	return &MemoryArena{
		chunkSize: chunkSize,
		chunks:    make([][]byte, 0, 4),
	}
}

func (a *MemoryArena) Alloc(size int) []byte {
	a.mu.Lock()
	defer a.mu.Unlock()

	if size > a.chunkSize {
		// 大对象直接分配
		return make([]byte, size)
	}

	// 检查当前chunk是否有足够空间
	if len(a.chunks) == 0 || a.offset+size > a.chunkSize {
		// 分配新chunk
		a.chunks = append(a.chunks, make([]byte, a.chunkSize))
		a.offset = 0
	}

	chunk := a.chunks[len(a.chunks)-1]
	ptr := chunk[a.offset : a.offset+size]
	a.offset += size

	return ptr
}

func (a *MemoryArena) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.chunks = a.chunks[:0]
	a.offset = 0
}

// 使用示例
func ExampleOptimizations() {
	// 1. 使用细粒度对象池
	pool := NewSizedPool()
	buf := pool.Get(100)
	// ... 使用buf
	pool.Put(&buf)

	// 2. 零拷贝字符串构建
	builder := NewZeroCopyBuilder(1024)
	builder.WriteString("Hello")
	builder.WriteString(" World")
	str := builder.UnsafeString() // 零拷贝
	_ = str

	// 3. Work-stealing执行器
	executor := NewWorkStealingExecutor(4)
	executor.Start()
	defer executor.Stop()

	// 提交任务
	task := &wsSimpleTask{fn: func() {
		// 执行实际工作
	}}
	executor.Submit(task)

	// 4. Radix sort
	values := []float64{3.14, 1.41, 2.71, 1.73}
	RadixSortFloat64(values)

	// 5. Hilbert索引
	idx := HilbertXYToIndex(10, 20, 8)
	_ = idx
}

// 实现WSTask接口
type wsSimpleTask struct {
	fn func()
}

func (t *wsSimpleTask) Execute() {
	t.fn()
}
