package config

import "testing"

func TestLoadRequiresExactSource(t *testing.T) {
	getenv := func(name string) string { return map[string]string{}[name] }
	if _, err := Load(getenv); err == nil {
		t.Fatal("expected missing source error")
	}
	getenv = func(name string) string { return map[string]string{"TOURNAMENT_SOURCE": "other"}[name] }
	if _, err := Load(getenv); err == nil {
		t.Fatal("expected unknown source error")
	}
}

func TestLoadAPIAndHTMLRequirementsAreSeparate(t *testing.T) {
	apiEnv := map[string]string{"TOURNAMENT_SOURCE": "api", "TOURNAMENT_API_TOKEN": "example-api-token"}
	api, err := Load(func(name string) string { return apiEnv[name] })
	if err != nil || api.Source != SourceAPI || api.APIToken != "example-api-token" {
		t.Fatalf("unexpected API config: %+v err=%v", api, err)
	}
	htmlEnv := map[string]string{"TOURNAMENT_SOURCE": "html", "TOURNAMENT_HTML_URL": "https://example.test/community", "TOURNAMENT_API_TOKEN": "unused-example-token"}
	html, err := Load(func(name string) string { return htmlEnv[name] })
	if err != nil || html.Source != SourceHTML || html.HTMLURL == "" {
		t.Fatalf("unexpected HTML config: %+v err=%v", html, err)
	}
	if _, err := Load(func(name string) string {
		if name == "TOURNAMENT_SOURCE" {
			return "html"
		}
		if name == "TOURNAMENT_API_TOKEN" {
			return "example-api-token"
		}
		return ""
	}); err == nil {
		t.Fatal("expected HTML URL requirement")
	}
}

func TestLoadRejectsInvalidHTMLStartURL(t *testing.T) {
	values := map[string]string{"TOURNAMENT_SOURCE": "html", "TOURNAMENT_HTML_URL": "file:///tmp/community"}
	if _, err := Load(func(name string) string { return values[name] }); err == nil {
		t.Fatal("expected invalid HTML URL error")
	}
}

func TestLoadAdminUIRequiresCredentialsAndAllowsConfiguredBind(t *testing.T) {
	values := map[string]string{"TOURNAMENT_SOURCE": "html", "TOURNAMENT_HTML_URL": "https://example.test/community", "ADMIN_UI_ENABLED": "true", "ADMIN_BIND_ADDR": "0.0.0.0:8081"}
	if _, err := Load(func(name string) string { return values[name] }); err == nil {
		t.Fatal("expected admin credential requirement")
	}
	values["ADMIN_USERNAME"] = "example-admin"
	values["ADMIN_PASSWORD"] = "example-password"
	config, err := Load(func(name string) string { return values[name] })
	if err != nil || !config.AdminUIEnabled || config.AdminUsername != "example-admin" || config.AdminPassword != "example-password" {
		t.Fatalf("merge UI config=%+v err=%v", config, err)
	}
}
