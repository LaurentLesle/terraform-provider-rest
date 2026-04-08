package functions

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"testing"

	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/nacl/box"
)

func TestNaclSealAnonymous_RoundTrip(t *testing.T) {
	// Generate a keypair
	recipientPub, recipientPriv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	plaintext := "my-secret-value"
	pubKeyB64 := base64.StdEncoding.EncodeToString(recipientPub[:])

	// Encrypt
	ciphertextB64, err := naclSealAnonymous(plaintext, pubKeyB64, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	// Decode
	ciphertext, err := base64.StdEncoding.DecodeString(ciphertextB64)
	if err != nil {
		t.Fatal(err)
	}

	// Decrypt using SealAnonymous's inverse: OpenAnonymous
	decrypted, ok := box.OpenAnonymous(nil, ciphertext, recipientPub, recipientPriv)
	if !ok {
		t.Fatal("decryption failed")
	}
	if string(decrypted) != plaintext {
		t.Errorf("got %q, want %q", decrypted, plaintext)
	}
}

func TestNaclSealAnonymous_EmptyPlaintext(t *testing.T) {
	recipientPub, recipientPriv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	pubKeyB64 := base64.StdEncoding.EncodeToString(recipientPub[:])

	ciphertextB64, err := naclSealAnonymous("", pubKeyB64, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	ciphertext, err := base64.StdEncoding.DecodeString(ciphertextB64)
	if err != nil {
		t.Fatal(err)
	}

	decrypted, ok := box.OpenAnonymous(nil, ciphertext, recipientPub, recipientPriv)
	if !ok {
		t.Fatal("decryption failed")
	}
	if string(decrypted) != "" {
		t.Errorf("got %q, want empty", decrypted)
	}
}

func TestNaclSealAnonymous_InvalidBase64(t *testing.T) {
	_, err := naclSealAnonymous("test", "not-valid-base64!!!", rand.Reader)
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}
}

func TestNaclSealAnonymous_WrongKeyLength(t *testing.T) {
	// 16-byte key instead of 32
	shortKey := base64.StdEncoding.EncodeToString(make([]byte, 16))
	_, err := naclSealAnonymous("test", shortKey, rand.Reader)
	if err == nil {
		t.Fatal("expected error for wrong key length")
	}
}

func TestNaclSealAnonymous_DifferentCiphertextEachCall(t *testing.T) {
	recipientPub, _, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	pubKeyB64 := base64.StdEncoding.EncodeToString(recipientPub[:])

	ct1, err := naclSealAnonymous("same-value", pubKeyB64, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ct2, err := naclSealAnonymous("same-value", pubKeyB64, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	if ct1 == ct2 {
		t.Error("two encryptions of the same plaintext should produce different ciphertexts")
	}
}

func TestNaclSealAnonymous_DeterministicWithFixedRand(t *testing.T) {
	// Use a deterministic "random" source to verify the function
	// produces consistent output when given the same entropy.
	seed := bytes.Repeat([]byte{0x42}, 64)

	// Generate a deterministic key from a known scalar
	var privKey [32]byte
	copy(privKey[:], bytes.Repeat([]byte{0x01}, 32))
	pubKeyBytes, err := curve25519.X25519(privKey[:], curve25519.Basepoint)
	if err != nil {
		t.Fatal(err)
	}
	pubKeyB64 := base64.StdEncoding.EncodeToString(pubKeyBytes)

	reader1 := bytes.NewReader(seed)
	reader2 := bytes.NewReader(seed)

	ct1, err := naclSealAnonymous("test", pubKeyB64, reader1)
	if err != nil {
		t.Fatal(err)
	}
	ct2, err := naclSealAnonymous("test", pubKeyB64, reader2)
	if err != nil {
		t.Fatal(err)
	}

	if ct1 != ct2 {
		t.Errorf("deterministic random should produce same ciphertext: %q != %q", ct1, ct2)
	}
}
