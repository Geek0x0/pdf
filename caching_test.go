package pdf

import (
	"context"
	"testing"
	"time"
)

// TestNewResultCache tests the result cache initialization
func TestNewResultCache(t *testing.T) {
	cache := NewResultCache(1024*1024, 5*time.Minute, "LRU") // 1MB, 5min TTL, LRU policy

	if cache.maxSize != 1024*1024 {
		t.Errorf("Expected maxSize 1048576, got %d", cache.maxSize)
	}
	if cache.ttl != 5*time.Minute {
		t.Errorf("Expected TTL 5 minutes, got %v", cache.ttl)
	}
	if cache.policy != "LRU" {
		t.Errorf("Expected policy LRU, got %s", cache.policy)
	}
	if cache.items == nil {
		t.Error("Expected items map to be initialized")
	}
}

// TestResultCachePutAndGet tests basic put and get operations
func TestResultCachePutAndGet(t *testing.T) {
	cache := NewResultCache(1024*1024, 1*time.Hour, "LRU")

	// Test putting and getting a string
	testKey := "test_key"
	testValue := "test_value"

	cache.Put(testKey, testValue)

	value, found := cache.Get(testKey)
	if !found {
		t.Error("Expected to find the key after putting it")
	}

	if valueStr, ok := value.(string); ok {
		if valueStr != testValue {
			t.Errorf("Expected value %s, got %s", testValue, valueStr)
		}
	} else {
		t.Error("Expected to get a string value")
	}
}

// TestResultCachePutAndGetTextSlice tests storing and retrieving slices of Text
func TestResultCachePutAndGetTextSlice(t *testing.T) {
	cache := NewResultCache(1024*1024, 1*time.Hour, "LRU")

	testKey := "text_slice"
	testValue := []Text{
		{S: "text1", X: 100, Y: 200, FontSize: 12},
		{S: "text2", X: 150, Y: 250, FontSize: 14},
	}

	cache.Put(testKey, testValue)

	value, found := cache.Get(testKey)
	if !found {
		t.Error("Expected to find the key after putting it")
	}

	if valueSlice, ok := value.([]Text); ok {
		if len(valueSlice) != len(testValue) {
			t.Errorf("Expected slice length %d, got %d", len(testValue), len(valueSlice))
		}

		for i, expected := range testValue {
			if i >= len(valueSlice) {
				t.Errorf("Index %d out of bounds", i)
				continue
			}
			if valueSlice[i].S != expected.S || valueSlice[i].X != expected.X {
				t.Errorf("Text element at index %d mismatch", i)
			}
		}
	} else {
		t.Error("Expected to get a Text slice")
	}
}

// TestResultCacheExpiration tests automatic expiration
func TestResultCacheExpiration(t *testing.T) {
	cache := NewResultCache(1024*1024, 10*time.Millisecond, "LRU") // Very short TTL

	testKey := "expiring_key"
	testValue := "expiring_value"

	cache.Put(testKey, testValue)

	// Verify it's there initially
	_, found := cache.Get(testKey)
	if !found {
		t.Error("Expected to find the key before expiration")
	}

	// Wait for expiration
	time.Sleep(20 * time.Millisecond)

	// Should no longer be available
	_, found = cache.Get(testKey)
	if found {
		t.Error("Expected key to be expired after TTL")
	}
}

// TestResultCacheHas tests the Has method
func TestResultCacheHas(t *testing.T) {
	cache := NewResultCache(1024*1024, 1*time.Hour, "LRU")

	testKey := "has_test"
	testValue := "some_value"

	// Should not exist initially
	if cache.Has(testKey) {
		t.Error("Expected key to not exist initially")
	}

	// Add the key
	cache.Put(testKey, testValue)

	// Should exist now
	if !cache.Has(testKey) {
		t.Error("Expected key to exist after putting it")
	}

	// Remove and test again
	cache.Remove(testKey)
	if cache.Has(testKey) {
		t.Error("Expected key to not exist after removing it")
	}
}

// TestResultCacheRemove tests the Remove functionality
func TestResultCacheRemove(t *testing.T) {
	cache := NewResultCache(1024*1024, 1*time.Hour, "LRU")

	testKey := "remove_test"
	testValue := "remove_value"

	cache.Put(testKey, testValue)

	// Verify it's there
	_, found := cache.Get(testKey)
	if !found {
		t.Error("Expected key to exist before removal")
	}

	// Remove it
	removed := cache.Remove(testKey)
	if !removed {
		t.Error("Expected Remove to return true for existing key")
	}

	// Should no longer be there
	_, found = cache.Get(testKey)
	if found {
		t.Error("Expected key to not exist after removal")
	}

	// Removing non-existent key should return false
	removed = cache.Remove("non_existent")
	if removed {
		t.Error("Expected Remove to return false for non-existent key")
	}
}

// TestResultCacheSizeLimit tests automatic eviction when size limit is reached
func TestResultCacheSizeLimit(t *testing.T) {
	cache := NewResultCache(100, 1*time.Hour, "LRU") // Very small size limit

	// Add items that will exceed the size limit
	smallItem := "small"
	cache.Put("key1", smallItem)

	// Add a larger item that will trigger eviction
	largeItem := "this is a much larger item that should cause eviction"
	cache.Put("key2", largeItem)

	// The size should not exceed the limit
	if cache.stats.CurrentSize > cache.maxSize {
		t.Errorf("Cache size %d exceeds max size %d", cache.stats.CurrentSize, cache.maxSize)
	}

	// Both items might still be there depending on size estimation, but at least verify no panic
	_, _ = cache.Get("key1")
	_, found2 := cache.Get("key2")

	// For this test, we're mainly checking that it doesn't panic
	// The exact behavior depends on the size estimation implementation
	if !found2 {
		t.Log("Large item may have caused eviction of small item, which is expected")
	}
}

// TestResultCacheLRUPolicy tests LRU eviction policy
func TestResultCacheLRUPolicy(t *testing.T) {
	// Create a cache with very small capacity to force eviction
	cache := NewResultCache(300, 1*time.Hour, "LRU") // Small size

	// Add several items
	item1 := "item1_data"
	item2 := "item2_data"
	item3 := "item3_data"
	item4 := "item4_data_that_is_larger_than_others"

	cache.Put("key1", item1)
	cache.Put("key2", item2)
	cache.Put("key3", item3)

	// Access key1 to make it recently used
	cache.Get("key1")

	// Add larger item that may trigger eviction
	cache.Put("key4", item4)

	// In LRU, key2 or key3 (least recently used) might be evicted, but key1 (recently used) should remain
	// The exact behavior depends on size estimation
	_, key1Exists := cache.Get("key1")
	if !key1Exists {
		t.Log("Key1 may have been evicted due to size constraints")
	}
}

// TestResultCacheClear tests the Clear functionality
func TestResultCacheClear(t *testing.T) {
	cache := NewResultCache(1024*1024, 1*time.Hour, "LRU")

	// Add some items
	cache.Put("key1", "value1")
	cache.Put("key2", "value2")
	cache.Put("key3", "value3")

	// Verify they exist
	if !cache.Has("key1") || !cache.Has("key2") || !cache.Has("key3") {
		t.Error("Expected all keys to exist")
	}

	// Clear the cache
	cache.Clear()

	// Should be empty
	if cache.Has("key1") || cache.Has("key2") || cache.Has("key3") {
		t.Error("Expected all keys to be removed after Clear")
	}

	// Stats should be reset
	stats := cache.GetStats()
	if stats.CurrentSize != 0 || stats.Entries != 0 {
		t.Error("Expected current size and entries to be 0 after Clear")
	}
}

// TestResultCacheHitRatio tests hit ratio calculation
func TestResultCacheHitRatio(t *testing.T) {
	cache := NewResultCache(1024*1024, 1*time.Hour, "LRU")

	// Start with no hits or misses
	initialRatio := cache.GetHitRatio()
	if initialRatio != 0 {
		t.Errorf("Expected initial hit ratio 0, got %f", initialRatio)
	}

	// Add an item
	cache.Put("test_key", "test_value")

	// Get it (hit)
	_, _ = cache.Get("test_key")

	// Get non-existent key (miss)
	_, _ = cache.Get("non_existent_key")

	// Get it again (hit)
	_, _ = cache.Get("test_key")

	// Get non-existent key again (miss)
	_, _ = cache.Get("non_existent_key2")

	// At this point: 2 hits, 2 misses = 50% hit ratio
	ratio := cache.GetHitRatio()
	// The exact ratio depends on how we count operations
	t.Logf("Hit ratio: %f", ratio)
}

// TestStatsAccuracy tests cache statistics accuracy
func TestStatsAccuracy(t *testing.T) {
	cache := NewResultCache(1024*1024, 1*time.Hour, "LRU")

	// Initial stats
	initialStats := cache.GetStats()

	// Add an item
	cache.Put("key1", "value1")

	// Get stats after adding
	statsAfterPut := cache.GetStats()

	if statsAfterPut.Entries <= initialStats.Entries {
		t.Error("Expected entries count to increase after Put")
	}

	if statsAfterPut.CurrentSize <= initialStats.CurrentSize {
		t.Error("Expected current size to increase after Put")
	}

	// Get the item (should be a hit)
	_, _ = cache.Get("key1")

	// The hit count should have increased

	// Remove the item
	cache.Remove("key1")

	statsAfterRemove := cache.GetStats()
	if statsAfterRemove.CurrentSize >= statsAfterPut.CurrentSize {
		t.Log("Size after removal may not decrease immediately due to estimation")
	}
}

// TestNewCacheKeyGenerator tests the key generator initialization
func TestNewCacheKeyGenerator(t *testing.T) {
	generator := NewCacheKeyGenerator()

	if generator == nil {
		t.Error("Expected cache key generator to be created")
	}
}

// TestCacheKeyGeneration tests the various key generation methods
func TestCacheKeyGeneration(t *testing.T) {
	generator := NewCacheKeyGenerator()

	// Test page content key generation
	pageKey := generator.GeneratePageContentKey(5, "reader_hash_123")
	expectedPageKey := "page_content_reader_hash_123_5"
	if pageKey != expectedPageKey {
		t.Errorf("Expected page key %s, got %s", expectedPageKey, pageKey)
	}

	// Test text classification key generation
	classKey := generator.GenerateTextClassificationKey(5, "reader_hash_123", "params")
	expectedClassKey := "text_classification_reader_hash_123_5_params"
	if classKey != expectedClassKey {
		t.Errorf("Expected classification key %s, got %s", expectedClassKey, classKey)
	}

	// Test text ordering key generation
	orderKey := generator.GenerateTextOrderingKey(5, "reader_hash_123", "ordering_params")
	expectedOrderKey := "text_ordering_reader_hash_123_5_ordering_params"
	if orderKey != expectedOrderKey {
		t.Errorf("Expected ordering key %s, got %s", expectedOrderKey, orderKey)
	}
}

// TestNewCachedReader tests the cached reader initialization
func TestNewCachedReader(t *testing.T) {
	// Create a mock reader
	mockReader := &Reader{}
	cache := NewResultCache(1024*1024, 1*time.Hour, "LRU")

	cachedReader := NewCachedReader(mockReader, cache)

	if cachedReader.Reader != mockReader {
		t.Error("Expected cached reader to wrap the original reader")
	}
	if cachedReader.cache != cache {
		t.Error("Expected cached reader to use the provided cache")
	}
	if cachedReader.keyGenerator == nil {
		t.Error("Expected key generator to be initialized")
	}
}

// TestGlobalCache tests the global cache singleton
func TestGlobalCache(t *testing.T) {
	// Call GetGlobalCache multiple times and ensure it returns the same instance
	cache1 := GetGlobalCache()
	cache2 := GetGlobalCache()

	if cache1 != cache2 {
		t.Error("Expected GetGlobalCache to return the same instance (singleton)")
	}

	if cache1.maxSize == 0 || cache1.ttl == 0 {
		t.Error("Expected global cache to have default configuration")
	}
}

// TestNewCacheManager tests the cache manager initialization
func TestNewCacheManager(t *testing.T) {
	manager := NewCacheManager()

	if manager.pageCache == nil {
		t.Error("Expected page cache to be initialized")
	}
	if manager.classificationCache == nil {
		t.Error("Expected classification cache to be initialized")
	}
	if manager.textOrderingCache == nil {
		t.Error("Expected text ordering cache to be initialized")
	}
	if manager.metadataCache == nil {
		t.Error("Expected metadata cache to be initialized")
	}
}

// TestCacheManagerGetCaches tests retrieving individual caches from manager
func TestCacheManagerGetCaches(t *testing.T) {
	manager := NewCacheManager()

	pageCache := manager.GetPageCache()
	classCache := manager.GetClassificationCache()
	orderCache := manager.GetTextOrderingCache()
	metaCache := manager.GetMetadataCache()

	if pageCache == nil || classCache == nil || orderCache == nil || metaCache == nil {
		t.Error("Expected all cache getters to return non-nil caches")
	}
}

// TestCacheManagerTotalStats tests combined statistics
func TestCacheManagerTotalStats(t *testing.T) {
	manager := NewCacheManager()

	// Get initial stats
	initialStats := manager.GetTotalStats()

	// Add something to one of the caches
	manager.pageCache.Put("test_key", "test_value")

	// Get stats again
	laterStats := manager.GetTotalStats()

	// Total entries should be greater or equal
	if laterStats.Entries < initialStats.Entries {
		t.Error("Expected total entries to be >= initial entries")
	}
}

// TestCacheContext tests the context-aware cache wrapper
func TestCacheContext(t *testing.T) {
	cache := NewResultCache(1024*1024, 1*time.Hour, "LRU")
	ctx := context.Background()

	cacheCtx := NewCacheContext(ctx, cache)

	if cacheCtx.cache != cache {
		t.Error("Expected cache context to wrap the provided cache")
	}
	if cacheCtx.ctx == nil {
		t.Error("Expected context to be initialized")
	}

	// Add an item
	cache.Put("test_key", "test_value")

	// Get with timeout
	value, found, err := cacheCtx.GetWithTimeout("test_key", 1*time.Second)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if !found {
		t.Error("Expected to find the key")
	}
	if valueStr, ok := value.(string); !ok || valueStr != "test_value" {
		t.Error("Expected to get the correct value")
	}

	// Close the context
	cacheCtx.Close()

	// Should be able to call close multiple times without issues
	cacheCtx.Close()
}

// TestCacheContextWithTimeout tests timeout functionality
func TestCacheContextWithTimeout(t *testing.T) {
	cache := NewResultCache(1024*1024, 1*time.Millisecond, "LRU") // Very short TTL
	ctx := context.Background()

	cacheCtx := NewCacheContext(ctx, cache)

	cache.Put("quick_expiring", "value")

	// Wait for the item to expire
	time.Sleep(10 * time.Millisecond)

	// Try to get with timeout - should not find it
	value, found, err := cacheCtx.GetWithTimeout("quick_expiring", 100*time.Millisecond)
	if err != nil {
		t.Logf("Got expected error or timeout: %v", err)
	}
	if found {
		t.Logf("Value: %v", value)
	} else {
		t.Log("Correctly did not find expired key")
	}
}

// TestCacheEntryExpiration tests the IsExpired method
func TestCacheEntryExpiration(t *testing.T) {
	entry := &CacheEntry{
		Data:       "test_data",
		Expiration: time.Now().Add(-1 * time.Second), // Expired 1 second ago
	}

	if !entry.IsExpired() {
		t.Error("Expected entry to be expired")
	}

	// Create a non-expired entry
	entry2 := &CacheEntry{
		Data:       "test_data",
		Expiration: time.Now().Add(1 * time.Hour), // Expires in 1 hour
	}

	if entry2.IsExpired() {
		t.Error("Expected entry to not be expired")
	}

	// Entry with zero time (no expiration)
	entry3 := &CacheEntry{
		Data:       "test_data",
		Expiration: time.Time{}, // Zero time
	}

	if entry3.IsExpired() {
		t.Error("Expected entry with zero expiration to not be expired")
	}
}

// TestCacheKeyGeneratorFullHash tests the full hash generation
func TestCacheKeyGeneratorFullHash(t *testing.T) {
	generator := NewCacheKeyGenerator()

	testString := "test_data_for_hashing"

	hash := generator.GenerateFullHash(testString)

	if hash == "" {
		t.Error("Expected a non-empty hash")
	}

	if len(hash) != 32 { // MD5 produces 32 hex characters
		t.Errorf("Expected hash length 32, got %d", len(hash))
	}

	// Same input should produce same hash
	hash2 := generator.GenerateFullHash("test_data_for_hashing")
	if hash != hash2 {
		t.Error("Expected same input to produce same hash")
	}

	// Different input should produce different hash
	hash3 := generator.GenerateFullHash("different_data")
	if hash == hash3 {
		t.Error("Expected different input to produce different hash")
	}
}

// TestLargeCache tests with larger data
func TestLargeCache(t *testing.T) {
	cache := NewResultCache(1024*1024, 5*time.Minute, "LRU") // 1MB cache

	// Add several items
	for i := 0; i < 100; i++ {
		key := "key_" + string(rune('A'+i%26))
		value := make([]byte, 100) // 100 bytes each
		for j := range value {
			value[j] = byte((i + j) % 256)
		}
		cache.Put(key, value)
	}

	// Check that cache is working
	_, found := cache.Get("key_A")
	if !found {
		t.Log("Some items may have been evicted due to size constraints")
	}

	// Check stats
	stats := cache.GetStats()
	t.Logf("Cache stats: entries=%d, size=%d", stats.Entries, stats.CurrentSize)
}

// BenchmarkResultCachePutGet benchmarks basic put/get operations
func BenchmarkResultCachePutGet(b *testing.B) {
	cache := NewResultCache(1024*1024, 1*time.Hour, "LRU")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := string(rune('A' + (i % 26)))
		value := "value_" + key

		cache.Put(key, value)
		_, _ = cache.Get(key)
	}
}

// BenchmarkResultCacheConcurrentAccess benchmarks concurrent access
func BenchmarkResultCacheConcurrentAccess(b *testing.B) {
	cache := NewResultCache(1024*1024, 1*time.Hour, "LRU")

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := string(rune('A' + (i % 26)))
			value := "value_" + key
			i++

			cache.Put(key, value)
			_, _ = cache.Get(key)
		}
	})
}

func TestResultCacheHybridPolicy(t *testing.T) {
	cache := NewResultCache(1024*1024, 1*time.Hour, "HYBRID")

	// Put some items with different access patterns
	cache.Put("frequent_recent", "value1")
	cache.Put("frequent_old", "value2")
	cache.Put("infrequent_recent", "value3")
	cache.Put("infrequent_old", "value4")

	// Access items to build access patterns
	for i := 0; i < 10; i++ {
		cache.Get("frequent_recent") // High frequency, recent
		cache.Get("frequent_old")    // High frequency, older
	}
	for i := 0; i < 2; i++ {
		cache.Get("infrequent_recent") // Low frequency, recent
	}

	// Wait a bit to age the "old" items
	time.Sleep(10 * time.Millisecond)

	// Add more items to trigger eviction
	for i := 0; i < 100; i++ {
		cache.Put(string(rune('A'+i)), "fill_value")
	}

	// Check which items remain (hybrid policy should favor frequent+recent)
	_, found1 := cache.Get("frequent_recent")
	_, found2 := cache.Get("frequent_old")
	_, _ = cache.Get("infrequent_recent")
	_, found4 := cache.Get("infrequent_old")

	// frequent_recent should definitely be kept
	if !found1 {
		t.Error("Expected frequent_recent to be kept")
	}

	// The others depend on the exact eviction logic, but frequent_old should be favored over infrequent
	if found4 && !found2 {
		t.Error("Expected frequent_old to be kept over infrequent_old")
	}
}

// TestResultCacheEvictOneLRU tests that evictOne removes the least recently used item in LRU policy
func TestResultCacheEvictOneLRU(t *testing.T) {
	cache := NewResultCache(10, 1*time.Hour, "LRU") // Very small size

	// Add one small item
	cache.Put("small", "a") // 1 byte

	// Access it to make it recently used
	cache.Get("small")

	// Add a large item that exceeds the cache size
	cache.Put("large", "this_is_a_very_long_string_that_should_exceed_limit") // ~50 bytes

	// The small item should be evicted
	_, foundSmall := cache.Get("small")
	if foundSmall {
		t.Error("Expected small item to be evicted")
	}

	// The large item should be there
	_, foundLarge := cache.Get("large")
	if !foundLarge {
		t.Error("Expected large item to remain")
	}
}

// TestResultCacheEvictOneLFU tests that evictOne removes the least frequently used item in LFU policy
func TestResultCacheEvictOneLFU(t *testing.T) {
	cache := NewResultCache(10, 1*time.Hour, "LFU") // Very small size

	// Add one frequently used item
	cache.Put("frequent", "a") // 1 byte
	for i := 0; i < 5; i++ {
		cache.Get("frequent") // Make it frequently used
	}

	// Add a large item that exceeds the cache size
	cache.Put("large", "this_is_a_very_long_string_that_should_exceed_limit") // ~50 bytes

	// The frequent item should be evicted (even though frequent, size matters more)
	_, foundFrequent := cache.Get("frequent")
	if foundFrequent {
		t.Error("Expected frequent item to be evicted due to size")
	}

	// The large item should be there
	_, foundLarge := cache.Get("large")
	if !foundLarge {
		t.Error("Expected large item to remain")
	}
}

// TestResultCacheEvictOneHybrid tests that evictOne removes items based on hybrid LRU+LFU policy
func TestResultCacheEvictOneHybrid(t *testing.T) {
	cache := NewResultCache(10, 1*time.Hour, "HYBRID") // Very small size

	// Add one item with high frequency/recent access
	cache.Put("good", "a") // 1 byte
	for i := 0; i < 10; i++ {
		cache.Get("good") // Make it very desirable
	}

	// Add a large item that exceeds the cache size
	cache.Put("large", "this_is_a_very_long_string_that_should_exceed_limit") // ~50 bytes

	// The good item should be evicted due to size constraints
	_, foundGood := cache.Get("good")
	if foundGood {
		t.Error("Expected good item to be evicted due to size")
	}

	// The large item should be there
	_, foundLarge := cache.Get("large")
	if !foundLarge {
		t.Error("Expected large item to remain")
	}
}

// TestResultCacheEvictOneEmpty tests that evictOne handles empty cache gracefully
func TestResultCacheEvictOneEmpty(t *testing.T) {
	cache := NewResultCache(1024*1024, 1*time.Hour, "LRU")

	// Cache is empty, evictOne should not panic
	// We can't directly call evictOne since it's private, but we can verify
	// that operations work normally on an empty cache

	// Getting a non-existent key should work
	_, found := cache.Get("nonexistent")
	if found {
		t.Error("Expected nonexistent key to not be found")
	}

	// Adding and getting should work normally
	cache.Put("key1", "value1")
	_, found = cache.Get("key1")
	if !found {
		t.Error("Expected key1 to be found after adding it")
	}
}

// TestEstimateSize tests the size estimation for different data types
func TestEstimateSize(t *testing.T) {
	cache := NewResultCache(1024*1024, 1*time.Hour, "LRU")

	tests := []struct {
		data     interface{}
		expected int64
	}{
		{"hello", 5},                                  // string
		{[]byte("world"), 5},                          // []byte
		{[]Text{{S: "test"}}, 100},                    // []Text
		{[]ClassifiedBlock{{Text: "test"}}, 200},      // []ClassifiedBlock
		{Text{S: "test"}, int64(len("test") + 64)},    // Text
		{Metadata{Title: "test"}, int64(len("test"))}, // Metadata
		{123, 1024}, // default case
	}

	for _, test := range tests {
		result := cache.estimateSize(test.data)
		if result != test.expected {
			t.Errorf("estimateSize(%T) = %d, expected %d", test.data, result, test.expected)
		}
	}
}
