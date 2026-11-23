package security

import "testing"

func TestIsCredentialPath(t *testing.T) {
	for _, p := range []string{
		".env",
		".env.local",
		".env.production",
		"config/.env",
		"svc/.ENV.Staging",
		".envrc",
		".netrc",
		"_netrc",
		".pgpass",
		".git-credentials",
		".npmrc",
		".pypirc",
		".aws/credentials",
		"deploy/id_rsa",
		"id_ed25519",
		"id_ecdsa",
		"id_dsa",
		"certs/server.pem",
		"tls/server.key",
		"store.p12",
		"store.pfx",
		"release.jks",
		"app.keystore",
	} {
		if !IsCredentialPath(p) {
			t.Errorf("IsCredentialPath(%q) = false, want true", p)
		}
	}
	for _, p := range []string{
		"",
		"main.go",
		"settings.cfg",
		".environments/prod.yaml",
		".envoy/config.yaml",
		"docs/env.md",
		"id_rsa.pub",
		"keys.go",
		"monkey.ts",
		"credentials.go",
		"internal/credentials/store.go",
		"pem.go",
	} {
		if IsCredentialPath(p) {
			t.Errorf("IsCredentialPath(%q) = true, want false", p)
		}
	}
}
