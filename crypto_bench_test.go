package pdf

import (
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
	"testing"
)

// BenchmarkAES256Encryption benchmarks AES-256 encryption performance
func BenchmarkAES256Encryption(b *testing.B) {
	// Setup encryption info for V=5
	info := PDFEncryptionInfo{
		Version:   EncryptionV5,
		Revision:  Revision5,
		Method:    MethodAESV3,
		KeyLength: 256,
		P:         0xFFFFFFFC,
		ID:        []byte("benchmark_document_id"),
		O:         make([]byte, 48),
		U:         make([]byte, 48),
		OE:        make([]byte, 32),
		UE:        make([]byte, 32),
		Perms:     make([]byte, 16),
	}

	engine := NewCryptoEngine(&info)

	// Set up a test key
	testKey := make([]byte, 32)
	for i := range testKey {
		testKey[i] = byte(i % 256)
	}
	engine.SetKey(testKey)

	// Test data of various sizes
	testSizes := []int{1 * 1024, 10 * 1024, 100 * 1024, 1024 * 1024} // 1KB, 10KB, 100KB, 1MB

	for _, size := range testSizes {
		testData := make([]byte, size)
		for i := range testData {
			testData[i] = byte(i % 256)
		}

		b.Run(fmt.Sprintf("Encrypt_%dKB", size/1024), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, err := engine.EncryptData(testData, 1, 0)
				if err != nil {
					b.Fatalf("Encryption failed: %v", err)
				}
			}
		})

		b.Run(fmt.Sprintf("Decrypt_%dKB", size/1024), func(b *testing.B) {
			encrypted, err := engine.EncryptData(testData, 1, 0)
			if err != nil {
				b.Fatalf("Setup encryption failed: %v", err)
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, err := engine.DecryptData(encrypted, 1, 0)
				if err != nil {
					b.Fatalf("Decryption failed: %v", err)
				}
			}
		})
	}
}

// BenchmarkSHA256Hashing benchmarks SHA-256 hashing performance for password validation
func BenchmarkSHA256Hashing(b *testing.B) {
	testData := []byte("test_password_for_benchmarking_sha256_performance")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h := sha256.New()
		h.Write(testData)
		_ = h.Sum(nil)
	}
}

// BenchmarkSHA384Hashing benchmarks SHA-384 hashing performance for R6 password validation
func BenchmarkSHA384Hashing(b *testing.B) {
	testData := []byte("test_password_for_benchmarking_sha384_performance")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h := sha512.New384()
		h.Write(testData)
		_ = h.Sum(nil)
	}
}

// BenchmarkSHA512Hashing benchmarks SHA-512 hashing performance for R6 password validation
func BenchmarkSHA512Hashing(b *testing.B) {
	testData := []byte("test_password_for_benchmarking_sha512_performance")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h := sha512.New()
		h.Write(testData)
		_ = h.Sum(nil)
	}
}
