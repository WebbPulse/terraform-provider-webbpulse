// Package kmsopenpgp signs provider releases as an OpenPGP version 4 RSA key whose
// private half is held by a crypto.Signer, in practice an AWS KMS key, so the
// release workflow never handles private key material.
//
// Terraform checks a provider's SHA256SUMS against a detached signature and an
// armored public key the registry serves. That key must carry a user ID with a
// self-signature made by the key itself, so the certificate is built here
// through the same signer. The key's creation time and the certificate's
// signature time are both the KMS key's creation time, and RSA PKCS #1 v1.5
// signatures are deterministic, so the same KMS key always yields the same
// certificate and the same key id.
package kmsopenpgp

import (
	"bytes"
	"crypto"
	"crypto/rsa"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

// Key is an OpenPGP RSA primary key whose signatures are made by a crypto.Signer.
type Key struct {
	public  *packet.PublicKey
	private *packet.PrivateKey
}

// NewKey wraps an RSA crypto.Signer as an OpenPGP key created at the given time.
//
// The creation time is part of the key's fingerprint, so it must be stable for
// the key id to be: pass the time the underlying key was created.
func NewKey(signer crypto.Signer, created time.Time) (*Key, error) {
	public, ok := signer.Public().(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("kmsopenpgp: the signing key is not an RSA key")
	}
	publicKey := packet.NewRSAPublicKey(created.UTC().Truncate(time.Second), public)
	return &Key{
		public:  publicKey,
		private: &packet.PrivateKey{PublicKey: *publicKey, PrivateKey: signer},
	}, nil
}

// KeyID is the long key id, sixteen upper case hex digits, as the registry and Terraform print it.
func (k *Key) KeyID() string {
	return k.public.KeyIdString()
}

// Fingerprint is the version 4 fingerprint in upper case hex.
func (k *Key) Fingerprint() string {
	return fmt.Sprintf("%X", k.public.Fingerprint)
}

// Certificate is the armored public key block: the key, one user ID and its positive self-certification.
func (k *Key) Certificate(name, email string) (string, error) {
	uid := packet.NewUserId(name, "", email)
	if uid == nil {
		return "", errors.New("kmsopenpgp: the user ID name or email holds characters OpenPGP does not allow")
	}
	primary := true
	selfSignature := k.signature(packet.SigTypePositiveCert, k.public.CreationTime)
	selfSignature.IsPrimaryId = &primary
	selfSignature.FlagsValid = true
	selfSignature.FlagSign = true
	selfSignature.FlagCertify = true
	if err := selfSignature.SignUserId(uid.Id, k.public, k.private, config(k.public.CreationTime)); err != nil {
		return "", fmt.Errorf("kmsopenpgp: self-certifying the user ID: %w", err)
	}
	entity := &openpgp.Entity{
		PrimaryKey: k.public,
		Identities: map[string]*openpgp.Identity{
			uid.Id: {
				Name:          uid.Id,
				UserId:        uid,
				SelfSignature: selfSignature,
				Signatures:    []*packet.Signature{selfSignature},
			},
		},
	}
	var out bytes.Buffer
	writer, err := armor.Encode(&out, openpgp.PublicKeyType, nil)
	if err != nil {
		return "", err
	}
	if err := entity.Serialize(writer); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	out.WriteString("\n")
	return out.String(), nil
}

// DetachSign writes a binary detached signature over message, made at the given time, as `gpg --detach-sign` would.
func (k *Key) DetachSign(w io.Writer, message io.Reader, at time.Time) error {
	signature := k.signature(packet.SigTypeBinary, at.UTC().Truncate(time.Second))
	cfg := config(signature.CreationTime)
	hash, err := signature.PrepareSign(cfg)
	if err != nil {
		return err
	}
	if _, err := io.Copy(hash, message); err != nil {
		return err
	}
	if err := signature.Sign(hash, k.private, cfg); err != nil {
		return fmt.Errorf("kmsopenpgp: signing: %w", err)
	}
	return signature.Serialize(w)
}

// signature is an unsigned version 4 SHA-256 signature packet of the given kind naming this key as its issuer.
func (k *Key) signature(kind packet.SignatureType, at time.Time) *packet.Signature {
	return &packet.Signature{
		Version:           4,
		SigType:           kind,
		PubKeyAlgo:        packet.PubKeyAlgoRSA,
		Hash:              crypto.SHA256,
		CreationTime:      at,
		IssuerKeyId:       &k.public.KeyId,
		IssuerFingerprint: k.public.Fingerprint,
	}
}

// config pins the hash and the clock and turns off the random salt notation, so signatures are reproducible.
func config(at time.Time) *packet.Config {
	randomized := false
	return &packet.Config{
		DefaultHash:                           crypto.SHA256,
		Time:                                  func() time.Time { return at },
		NonDeterministicSignaturesViaNotation: &randomized,
	}
}
