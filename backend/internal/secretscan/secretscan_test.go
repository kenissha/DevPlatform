package secretscan

import "testing"

func TestScan_DetectsKnownPatterns(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "private key block",
			content: "-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQEA...fake...\n-----END RSA PRIVATE KEY-----\n",
			want:    "private-key-block",
		},
		{
			name:    "generic private key block without algorithm prefix",
			content: "-----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBgkq...fake...\n-----END PRIVATE KEY-----\n",
			want:    "private-key-block",
		},
		{
			name:    "aws access key id",
			content: "aws_access_key_id = AKIAABCDEFGHIJKLMNOP",
			want:    "aws-access-key-id",
		},
		{
			name:    "aws temporary session access key id",
			content: "AWS_ACCESS_KEY_ID=ASIAABCDEFGHIJKLMNOP",
			want:    "aws-access-key-id",
		},
		{
			name:    "aws secret access key",
			content: "aws_secret_access_key = wJalrXUtnFEMIfakeSECRETfakeKEYfakeEXAMPLE",
			want:    "aws-secret-access-key",
		},
		{
			name:    "jwt",
			content: "Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.fakefakefakefakefake",
			want:    "jwt",
		},
		{
			name:    "dotnet connection string with password",
			content: `"DefaultConnection": "Server=sqlserver01;Database=Intranet;User Id=svc;Password=hunter2;"`,
			want:    "connection-string-password",
		},
		{
			name:    "dotnet connection string with pwd and data source",
			content: "Data Source=.;Initial Catalog=Intranet;Pwd=SuperSecret1;",
			want:    "connection-string-password",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Scan([]byte(tt.content))
			if !ok {
				t.Fatalf("Scan(%q) = ok=false, want match %q", tt.content, tt.want)
			}
			if got != tt.want {
				t.Errorf("Scan(%q) = %q, want %q", tt.content, got, tt.want)
			}
		})
	}
}

func TestScan_IgnoresBenignContent(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "plain readme prose",
			content: "# My Project\n\nThis is a small internal tool for managing repositories.\n",
		},
		{
			name:    "password mentioned descriptively without assignment",
			content: "Users must reset their password every 90 days per policy.",
		},
		{
			name:    "short unrelated key=value line",
			content: "log_level=debug\ntimeout=30\n",
		},
		{
			name:    "password field without a connection-string context",
			content: `{"password": "hint: use your email password"}`,
		},
		{
			name:    "empty content",
			content: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if name, ok := Scan([]byte(tt.content)); ok {
				t.Errorf("Scan(%q) = matched %q, want no match", tt.content, name)
			}
		})
	}
}
