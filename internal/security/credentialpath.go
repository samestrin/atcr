package security

import (
	"path"
	"path/filepath"
	"strings"
)

// credentialBasenames are file names (compared case-insensitively) that hold
// credentials by convention: netrc/pgpass/git-credentials passwords, package
// registry tokens, cloud CLI credentials and SSH private keys.
var credentialBasenames = map[string]bool{
	".envrc":           true,
	".netrc":           true,
	"_netrc":           true,
	".pgpass":          true,
	".git-credentials": true,
	".npmrc":           true,
	".pypirc":          true,
	"credentials":      true, // ~/.aws/credentials, gcloud and similar
	"id_rsa":           true,
	"id_dsa":           true,
	"id_ecdsa":         true,
	"id_ed25519":       true,
}

// credentialExts are key and keystore extensions (compared case-insensitively).
var credentialExts = map[string]bool{
	".pem":      true,
	".key":      true,
	".p12":      true,
	".pfx":      true,
	".jks":      true,
	".keystore": true,
}

// IsCredentialPath reports whether path names a file that conventionally holds
// secrets: a dotenv file (".env", ".env.<suffix>", the same rule the auto-fix
// guard applies), a credentials file such as .netrc or .aws/credentials, an SSH
// private key or a key/keystore file. The review payload drops these files
// before anything is sent to a model, whatever the ignore files say, so a
// committed .env never reaches a provider. Matching is on the base name only.
func IsCredentialPath(p string) bool {
	if p == "" {
		return false
	}
	base := strings.ToLower(path.Base(filepath.ToSlash(p)))
	if base == ".env" || strings.HasPrefix(base, ".env.") {
		return true
	}
	if credentialBasenames[base] {
		return true
	}
	return credentialExts[path.Ext(base)]
}
