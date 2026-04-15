package crypto

import (
	"testing"
)

func TestEncryptDecrypt(t *testing.T) {
	// 初始化加密
	key := "12345678901234567890123456789012" // 32 字节
	if err := InitEncryption(key); err != nil {
		t.Fatalf("InitEncryption failed: %v", err)
	}

	// 测试数据
	plaintext := "sk-proj-secret-api-key-12345"

	// 加密
	encrypted, err := Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	if encrypted == "" {
		t.Fatal("Encrypted data is empty")
	}

	// 解密
	decrypted, err := Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	// 验证
	if decrypted != plaintext {
		t.Fatalf("Decryption mismatch: got %s, want %s", decrypted, plaintext)
	}
}

func TestEncryptionDifferentOutputs(t *testing.T) {
	key := "12345678901234567890123456789012"
	if err := InitEncryption(key); err != nil {
		t.Fatalf("InitEncryption failed: %v", err)
	}

	plaintext := "same-secret"

	// 加密两次
	encrypted1, _ := Encrypt(plaintext)
	encrypted2, _ := Encrypt(plaintext)

	// 由于每次使用不同的 IV，密文应该不同
	if encrypted1 == encrypted2 {
		t.Fatal("Two encryptions of same plaintext should produce different ciphertexts")
	}

	// 但解密后应该相同
	decrypted1, _ := Decrypt(encrypted1)
	decrypted2, _ := Decrypt(encrypted2)

	if decrypted1 != plaintext || decrypted2 != plaintext {
		t.Fatal("Decryption failed")
	}
}

func TestDecryptTamperedData(t *testing.T) {
	key := "12345678901234567890123456789012"
	if err := InitEncryption(key); err != nil {
		t.Fatalf("InitEncryption failed: %v", err)
	}

	plaintext := "secret"
	encrypted, _ := Encrypt(plaintext)

	// 篡改密文（改变最后一个字符）
	tampered := encrypted[:len(encrypted)-1] + "X"

	// 尝试解密篡改的数据
	_, err := Decrypt(tampered)
	if err == nil {
		t.Fatal("Decryption should fail for tampered data")
	}
}

func TestInvalidKeyLength(t *testing.T) {
	// 测试无效的密钥长度
	err := InitEncryption("short")
	if err == nil {
		t.Fatal("InitEncryption should fail with short key")
	}
}

func TestGenerateKey(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	if len(key) != 32 {
		t.Fatalf("Generated key length is %d, want 32", len(key))
	}

	// 测试生成的密钥可用
	if err := InitEncryption(key); err != nil {
		t.Fatalf("Generated key is not valid: %v", err)
	}

	// 测试可以加密解密
	plaintext := "test"
	encrypted, _ := Encrypt(plaintext)
	decrypted, _ := Decrypt(encrypted)

	if decrypted != plaintext {
		t.Fatal("Encryption/Decryption with generated key failed")
	}
}

// TestEncryptDecryptWithKey covers the rotate-encryption-key path where two
// different keys are in play within a single process.
func TestEncryptDecryptWithKey(t *testing.T) {
	keyA := []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	keyB := []byte("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	plaintext := "rotate-me"

	// Round trip under the same key works.
	encA, err := EncryptWithKey(keyA, plaintext)
	if err != nil {
		t.Fatalf("EncryptWithKey(A): %v", err)
	}
	back, err := DecryptWithKey(keyA, encA)
	if err != nil {
		t.Fatalf("DecryptWithKey(A): %v", err)
	}
	if back != plaintext {
		t.Fatalf("round-trip mismatch: got %q want %q", back, plaintext)
	}

	// Ciphertext produced with keyA must not decrypt under keyB.
	if _, err := DecryptWithKey(keyB, encA); err == nil {
		t.Fatal("DecryptWithKey(B) must fail on ciphertext produced with A")
	}

	// Format parity with the global Encrypt: a ciphertext produced by
	// InitEncryption+Encrypt must decrypt via DecryptWithKey using the same
	// bytes. This is what rotate-encryption-key relies on.
	if err := InitEncryption(string(keyA)); err != nil {
		t.Fatalf("InitEncryption(A): %v", err)
	}
	encGlobal, err := Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	got, err := DecryptWithKey(keyA, encGlobal)
	if err != nil {
		t.Fatalf("DecryptWithKey on global ciphertext: %v", err)
	}
	if got != plaintext {
		t.Fatalf("cross-API mismatch: got %q want %q", got, plaintext)
	}
}

func TestEncryptWithKeyInvalidLength(t *testing.T) {
	if _, err := EncryptWithKey([]byte("short"), "x"); err == nil {
		t.Fatal("EncryptWithKey must reject non-32-byte keys")
	}
	if _, err := DecryptWithKey([]byte("short"), "x"); err == nil {
		t.Fatal("DecryptWithKey must reject non-32-byte keys")
	}
}
