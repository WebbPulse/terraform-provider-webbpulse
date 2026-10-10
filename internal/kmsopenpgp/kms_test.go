package kmsopenpgp

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/kms/types"
)

const testKeyARN = "arn:aws:kms:us-west-2:111111111111:key/00000000-0000-0000-0000-000000000000"

// fakeKMS answers the three KMS calls from an in-memory RSA key, the way KMS would.
type fakeKMS struct {
	key     *rsa.PrivateKey
	created time.Time
	usage   types.KeyUsageType
	signs   int
}

// DescribeKey reports the key's ARN, spec, usage and creation date.
func (f *fakeKMS) DescribeKey(_ context.Context, _ *kms.DescribeKeyInput, _ ...func(*kms.Options)) (*kms.DescribeKeyOutput, error) {
	return &kms.DescribeKeyOutput{KeyMetadata: &types.KeyMetadata{
		Arn:          aws.String(testKeyARN),
		KeySpec:      types.KeySpecRsa4096,
		KeyUsage:     f.usage,
		CreationDate: aws.Time(f.created),
	}}, nil
}

// GetPublicKey returns the public key as DER SubjectPublicKeyInfo.
func (f *fakeKMS) GetPublicKey(_ context.Context, _ *kms.GetPublicKeyInput, _ ...func(*kms.Options)) (*kms.GetPublicKeyOutput, error) {
	der, err := x509.MarshalPKIXPublicKey(&f.key.PublicKey)
	if err != nil {
		return nil, err
	}
	return &kms.GetPublicKeyOutput{PublicKey: der}, nil
}

// Sign signs a digest with PKCS #1 v1.5, refusing anything but a digest message under the key's ARN.
func (f *fakeKMS) Sign(_ context.Context, params *kms.SignInput, _ ...func(*kms.Options)) (*kms.SignOutput, error) {
	if params.MessageType != types.MessageTypeDigest || aws.ToString(params.KeyId) != testKeyARN {
		return nil, errors.New("unexpected sign request")
	}
	hashes := map[types.SigningAlgorithmSpec]crypto.Hash{
		types.SigningAlgorithmSpecRsassaPkcs1V15Sha256: crypto.SHA256,
		types.SigningAlgorithmSpecRsassaPkcs1V15Sha384: crypto.SHA384,
		types.SigningAlgorithmSpecRsassaPkcs1V15Sha512: crypto.SHA512,
	}
	f.signs++
	signature, err := rsa.SignPKCS1v15(rand.Reader, f.key, hashes[params.SigningAlgorithm], params.Message)
	if err != nil {
		return nil, err
	}
	return &kms.SignOutput{Signature: signature}, nil
}

// newFakeKMS is a fake holding a fresh 2048 bit key created an hour ago.
func newFakeKMS(t *testing.T) *fakeKMS {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return &fakeKMS{key: key, created: time.Now().Add(-time.Hour), usage: types.KeyUsageTypeSignVerify}
}

// TestSignatureVerifiesAgainstCertificate is what Terraform does at install: read the armored key, check the detached signature.
func TestSignatureVerifiesAgainstCertificate(t *testing.T) {
	ctx := context.Background()
	fake := newFakeKMS(t)
	key, err := Open(ctx, fake, "alias/provider-signing")
	if err != nil {
		t.Fatal(err)
	}
	armored, err := key.Certificate("WebbPulse Terraform provider staging", "releases@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(armored, "-----BEGIN PGP PUBLIC KEY BLOCK-----") {
		t.Fatalf("certificate is not armored: %q", armored[:40])
	}
	keyring, err := openpgp.ReadArmoredKeyRing(strings.NewReader(armored))
	if err != nil {
		t.Fatalf("reading the certificate: %v", err)
	}
	if len(keyring) != 1 || len(keyring[0].Identities) != 1 {
		t.Fatalf("want one entity with one identity, got %d", len(keyring))
	}
	if got := keyring[0].PrimaryKey.KeyIdString(); got != key.KeyID() {
		t.Fatalf("key id %s, certificate says %s", key.KeyID(), got)
	}

	sums := []byte("abc123  terraform-provider-webbpulse_0.0.1_linux_amd64.zip\n")
	var signature bytes.Buffer
	if err := key.DetachSign(&signature, bytes.NewReader(sums), time.Now()); err != nil {
		t.Fatal(err)
	}
	signer, err := openpgp.CheckDetachedSignature(keyring, bytes.NewReader(sums), bytes.NewReader(signature.Bytes()), nil)
	if err != nil {
		t.Fatalf("checking the signature: %v", err)
	}
	if signer.PrimaryKey.KeyIdString() != key.KeyID() {
		t.Fatalf("signed by %s, want %s", signer.PrimaryKey.KeyIdString(), key.KeyID())
	}

	tampered := append([]byte{}, sums...)
	tampered[0] = 'x'
	if _, err := openpgp.CheckDetachedSignature(keyring, bytes.NewReader(tampered), bytes.NewReader(signature.Bytes()), nil); err == nil {
		t.Fatal("a signature over other bytes verified")
	}
}

// TestCertificateIsStable proves a rerun publishes the same key id and the same armored block.
func TestCertificateIsStable(t *testing.T) {
	ctx := context.Background()
	fake := newFakeKMS(t)
	first, err := Open(ctx, fake, testKeyARN)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Open(ctx, fake, testKeyARN)
	if err != nil {
		t.Fatal(err)
	}
	a, err := first.Certificate("name", "releases@example.com")
	if err != nil {
		t.Fatal(err)
	}
	b, err := second.Certificate("name", "releases@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if a != b || first.KeyID() != second.KeyID() || first.Fingerprint() != second.Fingerprint() {
		t.Fatal("the same KMS key gave two different certificates")
	}
	if len(first.KeyID()) != 16 || !strings.HasSuffix(first.Fingerprint(), first.KeyID()) {
		t.Fatalf("key id %s is not the tail of fingerprint %s", first.KeyID(), first.Fingerprint())
	}
}

// TestOpenRefusesEncryptionKeys keeps a wrong key from silently producing a certificate.
func TestOpenRefusesEncryptionKeys(t *testing.T) {
	fake := newFakeKMS(t)
	fake.usage = types.KeyUsageTypeEncryptDecrypt
	if _, err := Open(context.Background(), fake, testKeyARN); err == nil {
		t.Fatal("an ENCRYPT_DECRYPT key was accepted")
	}
	if fake.signs != 0 {
		t.Fatal("a refused key was asked to sign")
	}
}
