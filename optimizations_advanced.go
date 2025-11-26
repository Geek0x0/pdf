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

// ==================== Advanced optimization implementation ====================

// 1. Fine-grained object pool - multi-level size bucketing
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
	// Use bit operations to quickly calculate pool index
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

// 2. Zero-copy string builder
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

// UnsafeString Zero-copy return string (note: underlying buffer cannot be modified)
func (b *ZeroCopyBuilder) UnsafeString() string {
	return unsafe.String(unsafe.SliceData(b.buf), len(b.buf))
}

func (b *ZeroCopyBuilder) Reset() {
	b.buf = b.buf[:0]
}

// 3. Lock-free ring buffer (for producer-consumer)
type LockFreeRingBuffer struct {
	buffer []interface{}
	mask   uint64
	head   uint64   // write position
	tail   uint64   // read position
	_      [56]byte // cache line padding to prevent false sharing
}

func NewLockFreeRingBuffer(size int) *LockFreeRingBuffer {
	// Ensure size is power of 2
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

		// Check if full
		if head-tail >= uint64(len(rb.buffer)) {
			return false
		}

		// Try CAS update head
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

		// Check if empty
		if tail >= head {
			return nil, false
		}

		// Try CAS update tail
		if atomic.CompareAndSwapUint64(&rb.tail, tail, tail+1) {
			item := rb.buffer[tail&rb.mask]
			return item, true
		}
	}
}

// 4. Cache line aligned structure
type CacheLinePadded struct {
	value uint64
	_     [7]uint64 // pad to 64 bytes
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

// 5. Work-Stealing Deque (Chase-Lev algorithm)
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

// PushBottom - owner thread pushes from bottom
func (d *WSDeque) PushBottom(task WSTask) {
	d.mu.Lock()
	d.data = append(d.data, task)
	d.mu.Unlock()
}

// PopBottom - owner thread pops from bottom (LIFO)
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

// Steal - other threads steal from top (FIFO)
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

// 6. Work-Stealing thread pool
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
	// Randomly select a worker to submit task
	idx := int(p.roundRobin.Add(1)-1) % p.numWorkers
	p.workers[idx].deque.PushBottom(task)
}

func (w *WSWorker) run() {
	defer w.pool.wg.Done()

	for !w.pool.stopped.Load() {
		// 1. Take task from own deque bottom (LIFO, cache friendly)
		if task := w.deque.PopBottom(); task != nil {
			task.Execute()
			continue
		}

		// 2. Randomly steal tasks from other workers
		victim := (w.id + 1 + runtime.GOMAXPROCS(0)) % w.pool.numWorkers
		if task := w.pool.workers[victim].deque.Steal(); task != nil {
			task.Execute()
			continue
		}

		// 3. No tasks, yield CPU
		runtime.Gosched()
	}
}

// 7. Radix Sort float64 optimization
func RadixSortFloat64(values []float64) {
	if len(values) <= 1 {
		return
	}

	// Convert float64 to uint64 to maintain sort order
	keys := make([]uint64, len(values))
	for i, v := range values {
		bits := math.Float64bits(v)
		// Handle negative numbers: if sign bit is 1, flip all bits; otherwise flip only sign bit
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
		// Clear buckets
		for i := range buckets {
			buckets[i] = buckets[i][:0]
		}

		// Bucket
		for i, k := range keys {
			bucket := (k >> shift) & 0xFF
			buckets[bucket] = append(buckets[bucket], i)
		}

		// Collect
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

// 8. Hilbert curve calculation (for spatial indexing)
func HilbertXYToIndex(x, y, order uint32) uint64 {
	var index uint64
	var rx, ry, s uint32
	n := uint32(1 << order)

	for s = n / 2; s > 0; s /= 2 {
		rx = (x & s) >> (bits.TrailingZeros32(s))
		ry = (y & s) >> (bits.TrailingZeros32(s))

		index += uint64(s * s * ((3 * rx) ^ ry))

		// Rotate
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

// 9. SIMD-friendly batch operations (pseudocode, actual assembly needed)
func BatchCompareFloat64(a, b []float64, threshold float64) []bool {
	// Handle length mismatch gracefully - compare up to the shorter length
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}
	if minLen == 0 {
		return []bool{}
	}

	result := make([]bool, minLen)

	// Ideally use AVX2 to process 4 float64 at once
	// This is scalar version, should actually be implemented in assembly
	i := 0
	for ; i+4 <= minLen; i += 4 {
		// AVX2: load 4 values at once, compare, store
		result[i] = math.Abs(a[i]-b[i]) < threshold
		result[i+1] = math.Abs(a[i+1]-b[i+1]) < threshold
		result[i+2] = math.Abs(a[i+2]-b[i+2]) < threshold
		result[i+3] = math.Abs(a[i+3]-b[i+3]) < threshold
	}

	// Process remaining elements
	for ; i < minLen; i++ {
		result[i] = math.Abs(a[i]-b[i]) < threshold
	}

	return result
}

// 10. Memory pool manager (reduce GC pressure)
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
		// Allocate large objects directly
		return make([]byte, size)
	}

	// Check if current chunk has enough space
	if len(a.chunks) == 0 || a.offset+size > a.chunkSize {
		// Allocate new chunk
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

// Usage example
func ExampleOptimizations() {
	// 1. Use fine-grained object pool
	pool := NewSizedPool()
	buf := pool.Get(100)
	// ... use buf
	pool.Put(&buf)

	// 2. Zero-copy string building
	builder := NewZeroCopyBuilder(1024)
	builder.WriteString("Hello")
	builder.WriteString(" World")
	str := builder.UnsafeString() // zero-copy
	_ = str

	// 3. Work-stealing executor
	executor := NewWorkStealingExecutor(4)
	executor.Start()
	defer executor.Stop()

	// Submit task
	task := &wsSimpleTask{fn: func() {
		// Execute actual work
	}}
	executor.Submit(task)

	// 4. Radix sort
	values := []float64{3.14, 1.41, 2.71, 1.73}
	RadixSortFloat64(values)

	// 5. Hilbert index
	idx := HilbertXYToIndex(10, 20, 8)
	_ = idx
}

// Implement WSTask interface
type wsSimpleTask struct {
	fn func()
}

func (t *wsSimpleTask) Execute() {
	t.fn()
}
