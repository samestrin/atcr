package payload

import (
	"strings"
	"testing"
)

// The five credential lines the v1.3.0 pre-release probe (EST-8260) found in
// a committed .env and settings.cfg. The built-in ruleset caught only the
// Anthropic and Slack keys; the unquoted password, the URL userinfo password
// and the AWS secret key went to the model unchanged. Values are assembled at
// runtime for the same reason as the planted values in secretscan_test.go.
var (
	probeDBPassword  = "Pr0dDbPa" + "ss2026x"
	probeURLPassword = "s3cr3tPg" + "Pass99"
	probeAWSSecret   = strings.Join([]string{"wJalrXUtn", "FEMI/K7MD", "ENG/bPxRfi", "CYEXAMPLEKEY"}, "")
	probeAnthropic   = "sk-" + "ant-api03-" + strings.Repeat("Ab3De5Fg7Hi9Jk1Lm3No5", 4) + "AA"
	probeSlack       = "xoxb-" + "123456789012-1234567890123-AbCdEfGhIjKlMnOpQrStUvWx"
)

func probePayload() string {
	return "diff --git a/.env b/.env\n" +
		"+DB_PASSWORD=" + probeDBPassword + "\n" +
		"+DATABASE_URL=postgres://admin:" + probeURLPassword + "@db.internal:5432/app\n" +
		"+ANTHROPIC_API_KEY=" + probeAnthropic + "\n" +
		"+SLACK_BOT_TOKEN=" + probeSlack + "\n" +
		"diff --git a/settings.cfg b/settings.cfg\n" +
		"+[default]\n" +
		"+aws_secret_access_key = " + probeAWSSecret + "\n"
}

func TestBuiltinRules_ProbeFiveLines(t *testing.T) {
	text := probePayload()
	hits := scanBuiltin(text)
	for _, secret := range []string{probeDBPassword, probeURLPassword, probeAWSSecret, probeAnthropic, probeSlack} {
		assertCovered(t, text, hits, secret)
	}
	out := RedactSecrets(text, hits)
	for _, secret := range []string{probeDBPassword, probeURLPassword, probeAWSSecret, probeAnthropic, probeSlack} {
		if strings.Contains(out, secret) {
			t.Errorf("secret survived redaction: %q", out)
		}
	}
	// Redaction keeps the key names and URL shape a reviewer needs.
	for _, keep := range []string{"DB_PASSWORD=", "postgres://admin:", "@db.internal:5432/app", "aws_secret_access_key = "} {
		if !strings.Contains(out, keep) {
			t.Errorf("redaction removed %q: %q", keep, out)
		}
	}
}

func TestBuiltinRules_UnquotedAssignmentShapes(t *testing.T) {
	cases := map[string]string{
		"export":     "export DB_PASSWORD=" + probeDBPassword + "\n",
		"yaml":       "  password: " + probeDBPassword + "\n",
		"ini":        "db_passwd = " + probeDBPassword + "\n",
		"comment":    "API_SECRET=" + "Zx9Kq2Lm8Np4" + "Rt7Vw3Yb6Cd1 # rotate monthly\n",
		"token":      "GITLAB_TOKEN=" + "glpat" + "Zx9Kq2Lm8Np4Rt7Vw3Yb" + "\n",
		"no-newline": "PASSWORD=" + probeDBPassword,
		"word":       "DB_PASSWORD=" + "MySecret" + "2026",
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			hits := scanBuiltin(text)
			if len(hits) == 0 {
				t.Fatalf("scanBuiltin(%q) found nothing", text)
			}
			if out := RedactSecrets(text, hits); !strings.Contains(out, SecretPlaceholder) {
				t.Errorf("not redacted: %q", out)
			}
		})
	}
}

func TestBuiltinRules_URLCredentialShapes(t *testing.T) {
	for _, text := range []string{
		"redis://:" + probeURLPassword + "@cache:6379/0",
		"amqp://guest:" + probeURLPassword + "@mq/vhost",
		`dsn := "mysql://root:` + probeURLPassword + `@tcp(db:3306)/app"`,
	} {
		hits := scanBuiltin(text)
		assertCovered(t, text, hits, probeURLPassword)
	}
}

// Known-good lines the new rules must leave alone: code that reads or names a
// secret without holding one, placeholders, env references and URLs with no
// password.
func TestBuiltinRules_UnquotedNoFalsePositives(t *testing.T) {
	clean := []string{
		"password = getPassword()",
		"token := tok.Next()",
		"secret = cfg.Secret",
		"Password: req.Password,",
		"token: string",
		"api_key = os.Getenv(\"API_KEY\")",
		"PASSWORD=${DB_PASSWORD}",
		"password: $DB_PASSWORD",
		"password: changeme",
		"password=password123",
		"DB_PASSWORD=",
		"password_file=/run/secrets/db_password",
		"passwordMinLength = 12",
		"tokenTTL = 3600s",
		"secretVersion: v2",
		"for token := range tokens {",
		"postgres://user:password@localhost:5432/db",
		"amqp://guest:guest@localhost:5672/",
		"redis://:secret@localhost:6379",
		"postgres://${PGUSER}:${PGPASSWORD}@db/app",
		"https://example.com/path?q=1",
		"git@github.com:samestrin/atcr.git",
		"https://user@example.com/repo.git",
		"aws_secret_access_key = <your-secret-key>",
		"aws_secret_access_key = ${AWS_SECRET_ACCESS_KEY}",
		"// the token: see docs/auth.md for rotation",
	}
	for _, text := range clean {
		if hits := scanBuiltin(text); len(hits) != 0 {
			t.Errorf("scanBuiltin(%q) = %+v, want no hit", text, hits)
		}
	}
}
