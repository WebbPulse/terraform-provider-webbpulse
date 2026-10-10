// kms-openpgp signs provider releases with an AWS KMS key as an OpenPGP key.
//
//	kms-openpgp public-key -key <id|arn|alias> -name <name> -email <email> -output <file>
//	kms-openpgp sign -key <id|arn|alias> -output <file.sig> <file>
//
// public-key writes the armored certificate and prints the long key id; sign
// writes a binary detached signature, as `gpg --detach-sign` does. Both read AWS
// credentials and the region from the default chain and need kms:DescribeKey and
// kms:GetPublicKey; both sign through kms:Sign. The key defaults to
// SIGNING_KMS_KEY_ID.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/WebbPulse/terraform-provider-webbpulse/internal/kmsopenpgp"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/kms"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "kms-openpgp:", err)
		os.Exit(1)
	}
}

// run dispatches one subcommand.
func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: kms-openpgp public-key|sign [flags]")
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	keyID := flags.String("key", os.Getenv("SIGNING_KMS_KEY_ID"), "KMS key id, ARN, alias name or alias ARN")
	output := flags.String("output", "", "file to write")
	name := flags.String("name", "", "user ID name (public-key)")
	email := flags.String("email", "", "user ID email (public-key)")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if *keyID == "" || *output == "" {
		return errors.New("-key (or SIGNING_KMS_KEY_ID) and -output are required")
	}
	key, err := openKey(ctx, *keyID)
	if err != nil {
		return err
	}
	switch args[0] {
	case "public-key":
		if *name == "" {
			return errors.New("public-key needs -name")
		}
		return writePublicKey(key, *name, *email, *output)
	case "sign":
		if flags.NArg() != 1 {
			return errors.New("sign takes exactly one file to sign")
		}
		return sign(key, flags.Arg(0), *output)
	default:
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

// openKey loads the default AWS configuration and reads the KMS key.
func openKey(ctx context.Context, keyID string) (*kmsopenpgp.Key, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("loading AWS configuration: %w", err)
	}
	return kmsopenpgp.Open(ctx, kms.NewFromConfig(cfg), keyID)
}

// writePublicKey writes the armored certificate and prints the key id on stdout.
func writePublicKey(key *kmsopenpgp.Key, name, email, output string) error {
	armored, err := key.Certificate(name, email)
	if err != nil {
		return err
	}
	if err := os.WriteFile(output, []byte(armored), 0o644); err != nil {
		return err
	}
	fmt.Println(key.KeyID())
	return nil
}

// sign writes the detached signature of input to output, made now.
func sign(key *kmsopenpgp.Key, input, output string) (err error) {
	message, err := os.Open(input)
	if err != nil {
		return err
	}
	defer func() { _ = message.Close() }()
	out, err := os.Create(output)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := out.Close(); err == nil {
			err = closeErr
		}
	}()
	return key.DetachSign(out, message, time.Now())
}
