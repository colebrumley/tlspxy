package sigv4

import "testing"

func TestTargetDefault(t *testing.T) {
	tr, err := NewTargetResolver("s3", "us-east-1", "", false)
	if err != nil {
		t.Fatalf("NewTargetResolver: %v", err)
	}
	// Even with an AWS-looking host, override is off so the default is used.
	got, err := tr.Resolve("dynamodb.eu-west-1.amazonaws.com")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Service != "s3" || got.Region != "us-east-1" || got.Host != "s3.us-east-1.amazonaws.com" || got.Scheme != "https" {
		t.Errorf("default target = %+v", got)
	}
}

func TestTargetCustomEndpoint(t *testing.T) {
	tr, err := NewTargetResolver("s3", "us-east-1", "https://minio.internal:9000", false)
	if err != nil {
		t.Fatalf("NewTargetResolver: %v", err)
	}
	got, _ := tr.Resolve("")
	if got.Host != "minio.internal:9000" || got.Scheme != "https" || got.Service != "s3" {
		t.Errorf("custom endpoint target = %+v", got)
	}
}

func TestTargetHostOverride(t *testing.T) {
	tr, err := NewTargetResolver("s3", "us-east-1", "", true)
	if err != nil {
		t.Fatalf("NewTargetResolver: %v", err)
	}
	cases := []struct {
		host       string
		wantSvc    string
		wantRegion string
		wantHost   string
	}{
		{"dynamodb.eu-west-1.amazonaws.com", "dynamodb", "eu-west-1", "dynamodb.eu-west-1.amazonaws.com"},
		{"s3.us-west-2.amazonaws.com:443", "s3", "us-west-2", "s3.us-west-2.amazonaws.com"},
		{"iam.amazonaws.com", "iam", "us-east-1", "iam.amazonaws.com"},
	}
	for _, c := range cases {
		got, err := tr.Resolve(c.host)
		if err != nil {
			t.Fatalf("Resolve(%q): %v", c.host, err)
		}
		if got.Service != c.wantSvc || got.Region != c.wantRegion || got.Host != c.wantHost {
			t.Errorf("Resolve(%q) = %+v, want svc=%s region=%s host=%s", c.host, got, c.wantSvc, c.wantRegion, c.wantHost)
		}
	}
}

func TestTargetHostOverrideNonAWSFallsBackToDefault(t *testing.T) {
	tr, err := NewTargetResolver("s3", "us-east-1", "", true)
	if err != nil {
		t.Fatalf("NewTargetResolver: %v", err)
	}
	// A non-AWS host with override on falls back to the configured default.
	got, err := tr.Resolve("example.com")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Service != "s3" {
		t.Errorf("expected fallback to default, got %+v", got)
	}
}
