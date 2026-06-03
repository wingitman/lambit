package awscli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigProfilesDetectsSSO(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	data := `[default]
region = eu-west-1

[profile work-sso]
sso_session = company
sso_account_id = 123456789012
sso_role_name = Developer
region = eu-west-1

[profile legacy-sso]
sso_start_url = https://example.awsapps.com/start
sso_region = eu-west-1

[sso-session company]
sso_start_url = https://example.awsapps.com/start
sso_region = eu-west-1
`
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
	profiles := LoadConfigProfiles(path)
	if !profiles["default"].Configured || profiles["default"].SSO {
		t.Fatalf("default profile = %#v", profiles["default"])
	}
	if !profiles["work-sso"].SSO {
		t.Fatalf("work-sso profile = %#v", profiles["work-sso"])
	}
	if !profiles["legacy-sso"].SSO {
		t.Fatalf("legacy-sso profile = %#v", profiles["legacy-sso"])
	}
	if _, ok := profiles["company"]; ok {
		t.Fatalf("sso-session should not be treated as login profile: %#v", profiles["company"])
	}
}
