package pdf

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type nopTask struct{}

func (nopTask) Execute() {}

type wgTask struct {
	wg *sync.WaitGroup
	fn func()
}

func (t *wgTask) Execute() {
	if t.fn != nil {
		t.fn()
	}
	if t.wg != nil {
		t.wg.Done()
	}
}

func TestSimdAndZeroCopyHelpers(t *testing.T) {
	if idx := FastStringSearch("hello world", "world"); idx != 6 {
		t.Fatalf("FastStringSearch returned %d", idx)
	}
	if idx := FastStringSearch("short", "longer"); idx != -1 {
		t.Fatalf("expected -1 for missing needle, got %d", idx)
	}
	if got := FastStringConcat("a", "b", "c"); got != "abc" {
		t.Fatalf("FastStringConcat got %q", got)
	}

	parts := ZeroCopyStringSlice([]byte("a,b,c"), []byte{','})
	if len(parts) != 3 || parts[1] != "b" {
		t.Fatalf("ZeroCopyStringSlice unexpected result: %v", parts)
	}

	omp := NewOptimizedMemoryPool(8)
	buf := omp.Get()
	buf = append(buf, 'x')
	omp.Put(&buf)
	omp2 := NewOptimizedMemoryPool(4)
	bufAgain := omp2.Get()
	omp2.Put(&bufAgain)

	if capEstimate := EstimateCapacity(10, 1.5); capEstimate < 10 {
		t.Fatalf("EstimateCapacity too small: %d", capEstimate)
	}

	sb := NewStringBuffer(4)
	sb.WriteString("ab")
	sb.WriteByte('c')
	sb.WriteBytes([]byte("d"))
	if sb.String() != "abcd" || sb.Len() != 4 || sb.Cap() < 4 {
		t.Fatalf("StringBuffer unexpected state: len=%d cap=%d str=%q", sb.Len(), sb.Cap(), sb.String())
	}
	if sb.StringCopy() != "abcd" || len(sb.Bytes()) != 4 {
		t.Fatalf("StringBuffer copy/bytes mismatch")
	}
	sb.Reset()
	if sb.Len() != 0 {
		t.Fatalf("StringBuffer Reset failed")
	}

	pool := NewStringPool()
	if pool.Intern("x") != pool.Intern("x") || pool.Size() != 1 {
		t.Fatalf("StringPool intern failed")
	}
	pool.Clear()
	if pool.Size() != 0 {
		t.Fatalf("StringPool clear failed")
	}

	isb := NewInplaceStringBuilder(2)
	isb.Append("hi")
	isb.Append("!")
	if built := isb.Build(); built != "hi!" {
		t.Fatalf("InplaceStringBuilder Build = %q", built)
	}
	if isb.Len() != 3 {
		t.Fatalf("InplaceStringBuilder Len = %d", isb.Len())
	}
	isb.Reset()
	if isb.Len() != 0 {
		t.Fatalf("InplaceStringBuilder Reset failed")
	}

	if CompareStringsZeroCopy("a", "b") != -1 {
		t.Fatalf("CompareStringsZeroCopy mismatch")
	}
	if !HasPrefixZeroCopy("foobar", "foo") || !HasSuffixZeroCopy("foobar", "bar") {
		t.Fatalf("prefix/suffix helpers failed")
	}
	if trimmed := TrimSpaceZeroCopy("  hi\t"); trimmed != "hi" {
		t.Fatalf("TrimSpaceZeroCopy = %q", trimmed)
	}
	split := SplitZeroCopy("a,b,c", ',')
	if len(split) != 3 || split[2] != "c" {
		t.Fatalf("SplitZeroCopy = %v", split)
	}
	if joined := JoinZeroCopy([]string{"a", "b", "c"}, "-"); joined != "a-b-c" {
		t.Fatalf("JoinZeroCopy = %q", joined)
	}

	if FastStringConcatZC("zero", "copy") != "zerocopy" {
		t.Fatalf("FastStringConcatZC failed")
	}
	byteSlices := StringSliceToByteSlice([]string{"one", "two"})
	if string(byteSlices[1]) != "two" {
		t.Fatalf("StringSliceToByteSlice mismatch")
	}
	if InternString("go") != InternString("go") {
		t.Fatalf("InternString did not reuse value")
	}
	ClearGlobalStringPool()

	if sub := SubstringZeroCopy("abcdef", 1, 4); sub != "bcd" {
		t.Fatalf("SubstringZeroCopy = %q", sub)
	}
	if BytesToString([]byte{}) != "" || string(StringToBytes("hi")) != "hi" {
		t.Fatalf("string/byte zero copy helpers failed")
	}
}

func TestAdvancedOptimizations(t *testing.T) {
	sp := NewSizedPool()
	buf := sp.Get(20)
	if len(buf) != 0 {
		t.Fatalf("SizedPool.Get should return empty slice")
	}
	sp.Put(&buf)

	builder := NewZeroCopyBuilder(8)
	builder.WriteString("go")
	builder.WriteByte('!')
	if builder.UnsafeString() != "go!" {
		t.Fatalf("ZeroCopyBuilder result mismatch")
	}
	builder.Reset()
	if builder.UnsafeString() != "" {
		t.Fatalf("ZeroCopyBuilder Reset failed")
	}

	rb := NewLockFreeRingBuffer(2)
	if !rb.Push(1) || !rb.Push(2) {
		t.Fatalf("LockFreeRingBuffer push failed")
	}
	if rb.Push(3) {
		t.Fatalf("expected push to fail when full")
	}
	if v, ok := rb.Pop(); !ok || v.(int) != 1 {
		t.Fatalf("LockFreeRingBuffer Pop mismatch: %v, %v", v, ok)
	}

	counter := NewCacheLineAlignedCounter(2)
	counter.Add(0, 3)
	if counter.Get(0) != 3 {
		t.Fatalf("CacheLineAlignedCounter incorrect")
	}

	deque := NewWSDeque(2)
	deque.PushBottom(nopTask{})
	deque.PushBottom(nopTask{})
	if deque.PopBottom() == nil {
		t.Fatalf("PopBottom returned nil")
	}
	if task := deque.Steal(); task == nil {
		t.Fatalf("Steal should return task")
	} else {
		task.Execute()
	}

	executor := NewWorkStealingExecutor(2)
	executor.Start()
	var completed atomic.Int32
	wg := sync.WaitGroup{}
	wg.Add(3)
	for i := 0; i < 3; i++ {
		executor.Submit(&wgTask{
			wg: &wg,
			fn: func() {
				completed.Add(1)
			},
		})
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("work stealing executor did not finish tasks")
	}
	executor.Stop()
	if completed.Load() != 3 {
		t.Fatalf("expected 3 tasks, got %d", completed.Load())
	}

	values := []float64{3.14, -1, 2.71, 0.5}
	RadixSortFloat64(values)
	for i := 1; i < len(values); i++ {
		if values[i] < values[i-1] {
			t.Fatalf("RadixSortFloat64 did not sort: %v", values)
		}
	}

	if idx := HilbertXYToIndex(1, 2, 3); idx == 0 {
		t.Fatalf("HilbertXYToIndex returned zero")
	}

	comp := BatchCompareFloat64([]float64{1, 2, 3, 4}, []float64{1, 2.2, 2.9, 4.5}, 0.1)
	if len(comp) != 4 || !comp[0] || comp[1] || comp[2] {
		t.Fatalf("BatchCompareFloat64 unexpected result: %v", comp)
	}

	arena := NewMemoryArena(8)
	if small := arena.Alloc(4); len(small) != 4 {
		t.Fatalf("MemoryArena small alloc failed")
	}
	if large := arena.Alloc(16); len(large) != 16 {
		t.Fatalf("MemoryArena large alloc failed")
	}
	arena.Reset()
	if ptr := arena.Alloc(2); len(ptr) != 2 {
		t.Fatalf("MemoryArena after Reset failed")
	}

	ExampleOptimizations()
	(&wsSimpleTask{fn: func() {}}).Execute()
}
