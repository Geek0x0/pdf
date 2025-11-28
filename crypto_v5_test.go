package pdf

import (
	"testing"
)

// TestV5EncryptionSupport tests AES-256 encryption (V=5) support
func TestV5EncryptionSupport(t *testing.T) {
	// Test PDFEncryptionInfo creation for V=5
	info := PDFEncryptionInfo{
		Version:   EncryptionV5,
		Revision:  Revision5,
		Method:    MethodAESV3,
		KeyLength: 256,
		P:         0xFFFFFFFC, // Standard permissions
		ID:        []byte("test_document_id_123456789"),
		O:         make([]byte, 48), // Owner hash for V=5
		U:         make([]byte, 48), // User hash for V=5
		OE:        make([]byte, 32), // Owner encryption key
		UE:        make([]byte, 32), // User encryption key
		Perms:     make([]byte, 16), // Encrypted permissions
	}

	// Test PasswordAuth creation
	auth := NewPasswordAuth(&info)
	if auth == nil {
		t.Fatal("Failed to create PasswordAuth")
	}

	// Test CryptoEngine creation
	engine := NewCryptoEngine(&info)
	if engine == nil {
		t.Fatal("Failed to create CryptoEngine")
	}

	// Test basic encryption/decryption with a known key
	testKey := make([]byte, 32) // 256-bit key
	for i := range testKey {
		testKey[i] = byte(i)
	}
	engine.SetKey(testKey)

	testData := []byte("Hello, World! This is a test message for AES-256 encryption.")
	encrypted, err := engine.EncryptData(testData, 0, 0)
	if err != nil {
		t.Fatalf("Encryption failed: %v", err)
	}

	decrypted, err := engine.DecryptData(encrypted, 0, 0)
	if err != nil {
		t.Fatalf("Decryption failed: %v", err)
	}

	if string(decrypted) != string(testData) {
		t.Fatalf("Decryption result mismatch: got %q, want %q", decrypted, testData)
	}

	t.Log("V=5 AES-256 encryption/decryption test passed")
}
