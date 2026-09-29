package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

// EnvOriginVerifySSMParameter is the environment variable the origin_verify_ssm_parameter name falls back to.
const EnvOriginVerifySSMParameter = "WEBBPULSE_TF_ORIGIN_VERIFY_SSM_PARAMETER"

// ssmParameterReader returns the decrypted value of one SSM parameter.
type ssmParameterReader func(ctx context.Context, name string) (string, error)

// readSSMParameter reads one parameter with decryption using the ambient AWS
// credentials and region. A parameter named by its ARN is read in the ARN's region.
func readSSMParameter(ctx context.Context, name string) (string, error) {
	var options []func(*awsconfig.LoadOptions) error
	if parsed, err := arn.Parse(name); err == nil && parsed.Region != "" {
		options = append(options, awsconfig.WithRegion(parsed.Region))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return "", fmt.Errorf("loading the AWS configuration: %w", err)
	}
	if cfg.Region == "" {
		return "", errors.New("no AWS region is configured: set AWS_REGION or name the parameter by its ARN")
	}
	out, err := ssm.NewFromConfig(cfg).GetParameter(ctx, &ssm.GetParameterInput{
		Name:           aws.String(name),
		WithDecryption: aws.Bool(true),
	})
	if err != nil {
		return "", err
	}
	if out.Parameter == nil || aws.ToString(out.Parameter.Value) == "" {
		return "", errors.New("the parameter has no value")
	}
	return aws.ToString(out.Parameter.Value), nil
}
