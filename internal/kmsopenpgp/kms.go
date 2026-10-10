package kmsopenpgp

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/kms/types"
)

// KMSClient is the part of the KMS API a signing key needs: kms:DescribeKey, kms:GetPublicKey and kms:Sign.
type KMSClient interface {
	DescribeKey(ctx context.Context, params *kms.DescribeKeyInput, optFns ...func(*kms.Options)) (*kms.DescribeKeyOutput, error)
	GetPublicKey(ctx context.Context, params *kms.GetPublicKeyInput, optFns ...func(*kms.Options)) (*kms.GetPublicKeyOutput, error)
	Sign(ctx context.Context, params *kms.SignInput, optFns ...func(*kms.Options)) (*kms.SignOutput, error)
}

// signingAlgorithms maps the digest OpenPGP hashed with to the KMS algorithm that signs it.
var signingAlgorithms = map[crypto.Hash]types.SigningAlgorithmSpec{
	crypto.SHA256: types.SigningAlgorithmSpecRsassaPkcs1V15Sha256,
	crypto.SHA384: types.SigningAlgorithmSpecRsassaPkcs1V15Sha384,
	crypto.SHA512: types.SigningAlgorithmSpecRsassaPkcs1V15Sha512,
}

// kmsSigner is a crypto.Signer that asks KMS to sign each digest with RSASSA PKCS #1 v1.5.
type kmsSigner struct {
	ctx    context.Context
	client KMSClient
	keyARN string
	public *rsa.PublicKey
}

// Public is the RSA public key KMS reported for the key.
func (s *kmsSigner) Public() crypto.PublicKey {
	return s.public
}

// Sign signs a precomputed digest in KMS; the random source is unused since KMS supplies none and PKCS #1 v1.5 needs none.
func (s *kmsSigner) Sign(_ io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	if _, pss := opts.(*rsa.PSSOptions); pss {
		return nil, errors.New("kmsopenpgp: OpenPGP RSA signatures use PKCS #1 v1.5, not PSS")
	}
	algorithm, ok := signingAlgorithms[opts.HashFunc()]
	if !ok {
		return nil, fmt.Errorf("kmsopenpgp: KMS cannot sign a %v digest", opts.HashFunc())
	}
	out, err := s.client.Sign(s.ctx, &kms.SignInput{
		KeyId:            aws.String(s.keyARN),
		Message:          digest,
		MessageType:      types.MessageTypeDigest,
		SigningAlgorithm: algorithm,
	})
	if err != nil {
		return nil, fmt.Errorf("kmsopenpgp: kms:Sign: %w", err)
	}
	return out.Signature, nil
}

// Open reads an asymmetric RSA SIGN_VERIFY KMS key, by id, ARN or alias, as an OpenPGP key created when the KMS key was.
func Open(ctx context.Context, client KMSClient, keyID string) (*Key, error) {
	described, err := client.DescribeKey(ctx, &kms.DescribeKeyInput{KeyId: aws.String(keyID)})
	if err != nil {
		return nil, fmt.Errorf("kmsopenpgp: kms:DescribeKey: %w", err)
	}
	meta := described.KeyMetadata
	if meta == nil || meta.Arn == nil || meta.CreationDate == nil {
		return nil, errors.New("kmsopenpgp: KMS described the key without its ARN or creation date")
	}
	if meta.KeyUsage != types.KeyUsageTypeSignVerify || !strings.HasPrefix(string(meta.KeySpec), "RSA_") {
		return nil, fmt.Errorf("kmsopenpgp: %s is a %s %s key, not an RSA SIGN_VERIFY key", *meta.Arn, meta.KeySpec, meta.KeyUsage)
	}
	published, err := client.GetPublicKey(ctx, &kms.GetPublicKeyInput{KeyId: meta.Arn})
	if err != nil {
		return nil, fmt.Errorf("kmsopenpgp: kms:GetPublicKey: %w", err)
	}
	parsed, err := x509.ParsePKIXPublicKey(published.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("kmsopenpgp: parsing the KMS public key: %w", err)
	}
	public, ok := parsed.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("kmsopenpgp: the KMS public key is not RSA")
	}
	return NewKey(&kmsSigner{ctx: ctx, client: client, keyARN: *meta.Arn, public: public}, *meta.CreationDate)
}
