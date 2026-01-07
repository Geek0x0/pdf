package pdf

import (
	"testing"
)

func TestAuthenticateUserR5_InvalidUELength_NoPanic(t *testing.T) {
	info := &PDFEncryptionInfo{
		Version:   EncryptionV5,
		Revision:  Revision5,
		Method:    MethodAESV3,
		KeyLength: 256,
		U:         make([]byte, 48),
		UE:        make([]byte, 30), // not a multiple of 16
		O:         make([]byte, 48),
		OE:        make([]byte, 32),
		Perms:     make([]byte, 16),
	}
	pa := NewPasswordAuth(info)

	if _, err := pa.authenticateUserR5("password"); err == nil {
		t.Fatalf("expected error for invalid UE length, got nil")
	}
}

func TestAuthenticateOwnerR5_InvalidOELength_NoPanic(t *testing.T) {
	info := &PDFEncryptionInfo{
		Version:   EncryptionV5,
		Revision:  Revision5,
		Method:    MethodAESV3,
		KeyLength: 256,
		U:         make([]byte, 48),
		UE:        make([]byte, 32),
		O:         make([]byte, 48),
		OE:        make([]byte, 18), // not a multiple of 16
		Perms:     make([]byte, 16),
	}
	pa := NewPasswordAuth(info)

	if _, err := pa.authenticateOwnerR5("password"); err == nil {
		t.Fatalf("expected error for invalid OE length, got nil")
	}
}

func TestValidatePermissions_InvalidPermsLength_NoPanic(t *testing.T) {
	info := &PDFEncryptionInfo{
		Version:   EncryptionV5,
		Revision:  Revision5,
		Method:    MethodAESV3,
		KeyLength: 256,
		U:         make([]byte, 48),
		UE:        make([]byte, 32),
		O:         make([]byte, 48),
		OE:        make([]byte, 32),
		Perms:     make([]byte, 10), // not a multiple of 16
		P:         0,
	}
	pa := NewPasswordAuth(info)
	// Mock a key of 32 bytes
	key := make([]byte, 32)
	if err := pa.ValidatePermissions(key); err == nil {
		t.Fatalf("expected error for invalid Perms length, got nil")
	}
}
