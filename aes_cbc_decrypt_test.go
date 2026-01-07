package pdf

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	crand "crypto/rand"
	"encoding/binary"
	"testing"
)

// padPKCS7 is a local helper for tests to generate ciphertexts
func padPKCS7(data []byte, blockSize int) []byte {
	padding := blockSize - (len(data) % blockSize)
	if padding == 0 {
		padding = blockSize
	}
	return append(data, bytes.Repeat([]byte{byte(padding)}, padding)...)
}

func TestDecryptString_AESCBC_InvalidBlockLen_NoPanic(t *testing.T) {
	baseKey := []byte("unit-test-key")
	p := objptr{id: 42, gen: 0}

	// IV must be 16 bytes; ciphertext intentionally set to non-multiple of block size
	iv := make([]byte, aes.BlockSize)
	badCiphertext := []byte{0x01} // 1 byte -> invalid for CBC
	input := append(iv, badCiphertext...)

	got := decryptString(baseKey, true, p, string(input))
	if got != string(input) {
		t.Fatalf("expected original input to be returned on invalid block length; got different output")
	}
}

func TestDecryptString_AESCBC_ValidBlock_PaddingRemoved(t *testing.T) {
	baseKey := []byte("unit-test-key")
	p := objptr{id: 7, gen: 0}

	// Derive the effective AES key exactly as decryptString does
	k := cryptKey(baseKey, true, p)

	block, err := aes.NewCipher(k)
	if err != nil {
		t.Fatalf("aes.NewCipher error: %v", err)
	}

	iv := make([]byte, aes.BlockSize)
	encr := cipher.NewCBCEncrypter(block, iv)

	plaintext := []byte("hello world")
	padded := padPKCS7(plaintext, aes.BlockSize)
	ciphertext := make([]byte, len(padded))
	encr.CryptBlocks(ciphertext, padded)

	// Assemble input as IV + ciphertext
	input := append(iv, ciphertext...)

	got := decryptString(baseKey, true, p, string(input))
	if got != string(plaintext) {
		t.Fatalf("expected decrypted plaintext without padding; got %q", got)
	}
}

func TestDecryptString_AESCBC_Fuzz_RandomIV_VariedPlaintext(t *testing.T) {
	const iterations = 200
	baseKey := []byte("unit-test-key")

	for i := 0; i < iterations; i++ {
		// Randomize objptr to vary derived key
		var idbuf [4]byte
		if _, err := crand.Read(idbuf[:]); err != nil {
			t.Fatalf("rand id: %v", err)
		}
		var genbuf [2]byte
		if _, err := crand.Read(genbuf[:]); err != nil {
			t.Fatalf("rand gen: %v", err)
		}
		p := objptr{id: binary.LittleEndian.Uint32(idbuf[:]), gen: binary.LittleEndian.Uint16(genbuf[:])}

		k := cryptKey(baseKey, true, p)

		block, err := aes.NewCipher(k)
		if err != nil {
			t.Fatalf("aes.NewCipher: %v", err)
		}

		// Random IV
		iv := make([]byte, aes.BlockSize)
		if _, err := crand.Read(iv); err != nil {
			t.Fatalf("rand iv: %v", err)
		}

		// Random plaintext length 0..512
		var lenbuf [2]byte
		if _, err := crand.Read(lenbuf[:]); err != nil {
			t.Fatalf("rand len: %v", err)
		}
		plen := int(binary.LittleEndian.Uint16(lenbuf[:]) % 513)
		plaintext := make([]byte, plen)
		if plen > 0 {
			if _, err := crand.Read(plaintext); err != nil {
				t.Fatalf("rand plaintext: %v", err)
			}
		}

		encr := cipher.NewCBCEncrypter(block, iv)
		padded := padPKCS7(plaintext, aes.BlockSize)
		ciphertext := make([]byte, len(padded))
		encr.CryptBlocks(ciphertext, padded)

		input := append(iv, ciphertext...)

		got := decryptString(baseKey, true, p, string(input))
		if got != string(plaintext) {
			t.Fatalf("iteration %d: decrypt mismatch: want %d bytes, got %d", i, len(plaintext), len(got))
		}
	}
}

func FuzzDecryptString_AESCBC(f *testing.F) {
	// Seed corpus
	f.Add([]byte("unit-test-key"), uint32(1), uint16(0), []byte("hello"), uint8(0))
	f.Add([]byte("another-key"), uint32(42), uint16(7), []byte(""), uint8(5))
	f.Add(make([]byte, 32), uint32(9999), uint16(65535), []byte("longer plaintext for fuzzing"), uint8(3))

	f.Fuzz(func(t *testing.T, baseKey []byte, id uint32, gen uint16, plaintext []byte, tail uint8) {
		p := objptr{id: id, gen: gen}
		k := cryptKey(baseKey, true, p)
		block, err := aes.NewCipher(k)
		if err != nil {
			t.Skipf("aes.NewCipher error: %v", err)
		}

		// Derive a deterministic 16-byte IV from inputs without extra dependencies
		iv := make([]byte, aes.BlockSize)
		for i := 0; i < aes.BlockSize; i++ {
			var b byte
			if i < len(plaintext) {
				b = plaintext[i]
			} else if i < len(baseKey) {
				b = baseKey[i]
			}
			iv[i] = b
		}

		encr := cipher.NewCBCEncrypter(block, iv)
		padded := padPKCS7(plaintext, aes.BlockSize)
		ciphertext := make([]byte, len(padded))
		encr.CryptBlocks(ciphertext, padded)

		input := append(iv, ciphertext...)

		// Randomly exercise invalid block length path by appending non-multiple of block size
		if int(tail)%aes.BlockSize != 0 && tail > 0 {
			extra := make([]byte, int(tail))
			inputInvalid := append(input, extra...)
			got := decryptString(baseKey, true, p, string(inputInvalid))
			if got != string(inputInvalid) {
				t.Fatalf("invalid length should return original; got len=%d want len=%d", len(got), len(inputInvalid))
			}
		} else {
			got := decryptString(baseKey, true, p, string(input))
			if got != string(plaintext) {
				t.Fatalf("plaintext mismatch: want %d got %d", len(plaintext), len(got))
			}
		}
	})
}
