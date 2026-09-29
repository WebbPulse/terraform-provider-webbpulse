package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/WebbPulse/terraform-provider-webbpulse/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const syntheticSSMValue = "synthetic-from-ssm"

// fakeSSM answers GetParameter over the AWS JSON 1.1 protocol and records each call.
type fakeSSM struct {
	mu        sync.Mutex
	names     []string
	decrypted []bool
	regions   []string
	missing   bool
}

func (f *fakeSSM) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Header.Get("X-Amz-Target") != "AmazonSSM.GetParameter" {
		http.Error(w, "unexpected target", http.StatusBadRequest)
		return
	}
	var body struct {
		Name           string
		WithDecryption bool
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	region := ""
	if match := regexp.MustCompile(`Credential=[^/]+/[^/]+/([^/]+)/ssm/`).FindStringSubmatch(req.Header.Get("Authorization")); match != nil {
		region = match[1]
	}
	f.mu.Lock()
	f.names = append(f.names, body.Name)
	f.decrypted = append(f.decrypted, body.WithDecryption)
	f.regions = append(f.regions, region)
	f.mu.Unlock()

	w.Header().Set("Content-Type", "application/x-amz-json-1.1")
	if f.missing {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(w, `{"__type":"ParameterNotFound","message":"parameter not found"}`)
		return
	}
	value := "AQICAHencrypted"
	if body.WithDecryption {
		value = syntheticSSMValue
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"Parameter": map[string]any{"Name": body.Name, "Type": "SecureString", "Value": value, "Version": 1},
	})
}

func (f *fakeSSM) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.names)
}

// useFakeSSM points the AWS SDK at a fake SSM endpoint with synthetic
// credentials, and isolates it from any shared AWS configuration.
func useFakeSSM(t *testing.T) *fakeSSM {
	t.Helper()
	fake := &fakeSSM{}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)

	missing := filepath.Join(t.TempDir(), "absent")
	for name, value := range map[string]string{
		"AWS_ENDPOINT_URL_SSM":        server.URL,
		"AWS_ACCESS_KEY_ID":           "AKIASYNTHETICTEST",
		"AWS_SECRET_ACCESS_KEY":       "synthetic-test-only",
		"AWS_SESSION_TOKEN":           "",
		"AWS_REGION":                  "us-west-2",
		"AWS_DEFAULT_REGION":          "",
		"AWS_PROFILE":                 "",
		"AWS_CONFIG_FILE":             missing,
		"AWS_SHARED_CREDENTIALS_FILE": missing,
		"AWS_EC2_METADATA_DISABLED":   "true",
	} {
		t.Setenv(name, value)
	}
	t.Setenv(EnvOriginVerify, "")
	t.Setenv(EnvOriginVerifySSMParameter, "")
	return fake
}

// stateNeverHolds fails when any attribute in state carries the value.
func stateNeverHolds(value string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		for _, module := range s.Modules {
			for address, rs := range module.Resources {
				for key, attribute := range rs.Primary.Attributes {
					if strings.Contains(attribute, value) {
						return fmt.Errorf("%s.%s holds the access gate value", address, key)
					}
				}
			}
		}
		return nil
	}
}

func expectEveryRequestCarries(t *testing.T, recorder *gateRecorder, want string) {
	t.Helper()
	for i, value := range recorder.seen {
		if value != want {
			t.Errorf("request %d carried %s %q, want %q", i, client.OriginVerifyHeader, value, want)
		}
	}
}

// TestOriginVerifyFromSSMParameterIsSentOnEveryRequest checks the configured
// parameter is read once with decryption and its value reaches every request
// but never the state.
func TestOriginVerifyFromSSMParameterIsSentOnEveryRequest(t *testing.T) {
	fake := useFakeSSM(t)
	recorder := runGateLifecycleWithCheck(t,
		`  origin_verify_ssm_parameter = "/synthetic/access-gate/origin-verify"`,
		stateNeverHolds(syntheticSSMValue))
	expectEveryRequestCarries(t, recorder, syntheticSSMValue)

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.names) == 0 {
		t.Fatal("the provider never read the parameter")
	}
	for i, name := range fake.names {
		if name != "/synthetic/access-gate/origin-verify" {
			t.Errorf("read %d asked for %q", i, name)
		}
		if !fake.decrypted[i] {
			t.Errorf("read %d did not ask for decryption", i)
		}
		if fake.regions[i] != "us-west-2" {
			t.Errorf("read %d was signed for %q, want the ambient region", i, fake.regions[i])
		}
	}
}

// TestOriginVerifySSMParameterFallsBackToTheEnvironment checks the parameter
// name is taken from its environment variable when the attribute is left out.
func TestOriginVerifySSMParameterFallsBackToTheEnvironment(t *testing.T) {
	fake := useFakeSSM(t)
	t.Setenv(EnvOriginVerifySSMParameter, "/synthetic/from-env")
	recorder := runGateLifecycle(t, "")
	expectEveryRequestCarries(t, recorder, syntheticSSMValue)
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.names) == 0 || fake.names[0] != "/synthetic/from-env" {
		t.Errorf("the provider read %v, want the parameter named in the environment", fake.names)
	}
}

// TestOriginVerifySSMParameterARNUsesItsRegion checks a parameter named by its
// ARN is read in the ARN's region rather than the ambient one.
func TestOriginVerifySSMParameterARNUsesItsRegion(t *testing.T) {
	fake := useFakeSSM(t)
	parameterARN := "arn:aws:ssm:us-east-1:123456789012:parameter/synthetic/origin-verify"
	value, err := readSSMParameter(context.Background(), parameterARN)
	if err != nil {
		t.Fatal(err)
	}
	if value != syntheticSSMValue {
		t.Error("the read did not return the decrypted value")
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.regions) != 1 || fake.regions[0] != "us-east-1" {
		t.Errorf("the read was signed for %v, want us-east-1", fake.regions)
	}
}

// TestOriginVerifyDirectValueWinsOverSSMParameter checks a direct value, from
// the attribute or the environment, is used and the parameter is never read.
func TestOriginVerifyDirectValueWinsOverSSMParameter(t *testing.T) {
	for name, setup := range map[string]struct {
		env   string
		extra string
		want  string
	}{
		"attribute":   {extra: "  origin_verify = \"synthetic-direct\"\n", want: "synthetic-direct"},
		"environment": {env: "synthetic-from-env", want: "synthetic-from-env"},
	} {
		t.Run(name, func(t *testing.T) {
			fake := useFakeSSM(t)
			t.Setenv(EnvOriginVerify, setup.env)
			recorder := runGateLifecycle(t, setup.extra+`  origin_verify_ssm_parameter = "/synthetic/unused"`)
			expectEveryRequestCarries(t, recorder, setup.want)
			if calls := fake.calls(); calls != 0 {
				t.Errorf("the provider read SSM %d times although a direct value was set", calls)
			}
		})
	}
}

// TestOriginVerifySSMParameterReadFailureIsAnError checks a failed read stops
// configuration with an error naming the parameter.
func TestOriginVerifySSMParameterReadFailureIsAnError(t *testing.T) {
	fake := useFakeSSM(t)
	fake.missing = true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("the provider called the API although the gate value could not be read")
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: fmt.Sprintf(`
provider "webbpulse" {
  host                        = %q
  token                       = "synthetic-test-only"
  origin_verify_ssm_parameter = "/synthetic/missing"
}

data "webbpulse_workspaces" "all" {}
`, server.URL),
			ExpectError: regexp.MustCompile(`(?s)Cannot\s+read\s+the\s+access\s+gate\s+value\s+from\s+SSM.*/synthetic/missing.*ParameterNotFound`),
		}},
	})
}

// TestOriginVerifyUsesTheConfiguredReader checks the provider reads through its
// configured reader and reports the reader's error.
func TestOriginVerifyUsesTheConfiguredReader(t *testing.T) {
	t.Setenv(EnvOriginVerify, "")
	t.Setenv(EnvOriginVerifySSMParameter, "")
	reads := 0
	factories := map[string]func() (tfprotov6.ProviderServer, error){
		"webbpulse": providerserver.NewProtocol6WithError(&webbpulseProvider{
			version: "test",
			readSSM: func(context.Context, string) (string, error) {
				reads++
				return "", errors.New("access denied")
			},
		}),
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: `
provider "webbpulse" {
  host                        = "https://api.example.invalid"
  token                       = "synthetic-test-only"
  origin_verify_ssm_parameter = "/synthetic/denied"
}

data "webbpulse_workspaces" "all" {}
`,
			ExpectError: regexp.MustCompile(`access\s+denied`),
		}},
	})
	if reads == 0 {
		t.Error("the configured reader was never called")
	}
}
