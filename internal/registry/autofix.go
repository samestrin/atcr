package registry

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// AutoFixConfig is the optional `auto_fix:` block in .atcr/config.yaml that
// supplies the config-derived backend pieces for the opt-in `--auto-fix` flow
// (Sprint 17.0, Story 6). Its mere presence does NOT enable anything — the
// `--auto-fix` flag must be passed AND cmd/atcr's validateAutoFixBackend gate
// must find every required piece present (apply target, validation command, and
// GitHub token/repo resolved from flags/env, not this block). A nil block means
// the config-derived pieces fall back to their defaults (apply target = repo
// root; validation command = the Go build default when a go.mod is present).
type AutoFixConfig struct {
	// ApplyTarget is the working-tree path the patch is applied to, resolved
	// against the repo root when relative. Empty defaults to the repo root.
	ApplyTarget string `yaml:"apply_target,omitempty"`
	// ValidateCommand is the post-apply validation command as an explicit argv
	// (never a shell string), e.g. [go, build, ./...]. Empty falls back to
	// verify.ResolveValidateCommand's single built-in default.
	ValidateCommand []string `yaml:"validate_command,omitempty"`
	// ValidateTimeout bounds one validation run, as a Go duration string (e.g.
	// "2m"). Empty inherits the gate's ~2 min default (TD-008); a zero/negative
	// value is rejected so a caller never gets an immediate false TimedOut.
	ValidateTimeout string `yaml:"validate_timeout,omitempty"`
}

// Validate checks the auto_fix block at config-load time. A nil block is valid
// (the flow simply inherits defaults). When present, the validation command must
// contain no empty tokens and the timeout, if set, must parse as a positive Go
// duration — so a malformed backend fails at load rather than mid-run.
func (a *AutoFixConfig) Validate() error {
	if a == nil {
		return nil
	}
	for _, tok := range a.ValidateCommand {
		if strings.TrimSpace(tok) == "" {
			return errors.New("auto_fix.validate_command must not contain empty tokens")
		}
	}
	if t := strings.TrimSpace(a.ValidateTimeout); t != "" {
		d, err := time.ParseDuration(t)
		if err != nil {
			return fmt.Errorf("auto_fix.validate_timeout %q is not a valid duration (e.g. \"2m\"): %w", a.ValidateTimeout, err)
		}
		if d <= 0 {
			return fmt.Errorf("auto_fix.validate_timeout must be positive, got %q", a.ValidateTimeout)
		}
	}
	return nil
}
