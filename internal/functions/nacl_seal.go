package functions

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"

	"github.com/hashicorp/terraform-plugin-framework/function"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"golang.org/x/crypto/nacl/box"
)

var _ function.Function = &NaclSealFunction{}

type NaclSealFunction struct{}

func NewNaclSealFunction() function.Function {
	return &NaclSealFunction{}
}

func (f *NaclSealFunction) Metadata(_ context.Context, _ function.MetadataRequest, resp *function.MetadataResponse) {
	resp.Name = "nacl_seal"
}

func (f *NaclSealFunction) Definition(_ context.Context, _ function.DefinitionRequest, resp *function.DefinitionResponse) {
	resp.Definition = function.Definition{
		Summary: "Encrypt a string using NaCl sealed-box encryption (crypto_box_seal)",
		Description: "Encrypts plaintext using the recipient's public key with NaCl anonymous " +
			"sealed boxes (crypto_box_seal from libsodium). The public key must be provided " +
			"as a base64-encoded 32-byte Curve25519 key. Returns the ciphertext as a " +
			"base64-encoded string. Used by the GitHub Actions Secrets API.",
		Parameters: []function.Parameter{
			function.StringParameter{
				Name:        "plaintext",
				Description: "The secret value to encrypt.",
			},
			function.StringParameter{
				Name:        "public_key",
				Description: "The recipient's public key, base64-encoded (32-byte Curve25519 key).",
			},
		},
		Return: function.StringReturn{},
	}
}

func (f *NaclSealFunction) Run(ctx context.Context, req function.RunRequest, resp *function.RunResponse) {
	var plaintext string
	var publicKeyB64 string

	resp.Error = function.ConcatFuncErrors(
		req.Arguments.Get(ctx, &plaintext, &publicKeyB64),
	)
	if resp.Error != nil {
		return
	}

	ciphertext, err := naclSealAnonymous(plaintext, publicKeyB64, deterministicReader(plaintext, publicKeyB64))
	if err != nil {
		resp.Error = function.NewFuncError(fmt.Sprintf("nacl_seal failed: %s", err))
		return
	}

	resp.Error = function.ConcatFuncErrors(
		resp.Result.Set(ctx, types.StringValue(ciphertext)),
	)
}

func naclSealAnonymous(plaintext, publicKeyB64 string, randReader io.Reader) (string, error) {
	pubKeyBytes, err := base64.StdEncoding.DecodeString(publicKeyB64)
	if err != nil {
		return "", fmt.Errorf("invalid base64 public key: %w", err)
	}
	if len(pubKeyBytes) != 32 {
		return "", fmt.Errorf("public key must be 32 bytes, got %d", len(pubKeyBytes))
	}

	var recipientPubKey [32]byte
	copy(recipientPubKey[:], pubKeyBytes)

	sealed, err := box.SealAnonymous(nil, []byte(plaintext), &recipientPubKey, randReader)
	if err != nil {
		return "", fmt.Errorf("encryption failed: %w", err)
	}

	return base64.StdEncoding.EncodeToString(sealed), nil
}

// deterministicReader returns an io.Reader that produces exactly 32 bytes
// derived from the inputs via SHA-256. This makes the sealed-box output
// deterministic for the same (plaintext, publicKey) pair, which satisfies
// Terraform's requirement that provider functions are pure.
func deterministicReader(plaintext, publicKeyB64 string) io.Reader {
	h := sha256.New()
	h.Write([]byte(plaintext))
	h.Write([]byte{0x00})
	h.Write([]byte(publicKeyB64))
	return bytes.NewReader(h.Sum(nil))
}
