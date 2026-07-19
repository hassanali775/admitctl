package tenant

import (
	"fmt"
	"regexp"
	"strings"
)

// idPattern mirrors the DNS-label rules Kubernetes namespaces use:
// lowercase alphanumerics and hyphens, 3-63 chars, must start and end
// with an alphanumeric. Tenant IDs often end up embedded in URLs,
// resource names, and log labels, so this keeps them safe everywhere
// without a separate slugify step.
var idPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{1,61}[a-z0-9])?$`)

// dataSchemaVersionPattern accepts "v1", "v2", "v1.3", etc. Onboarding
// pins a tenant to a schema version; the version format itself is
// intentionally loose since schema versioning policy may change.
var dataSchemaVersionPattern = regexp.MustCompile(`^v[0-9]+(\.[0-9]+)?$`)

var validAuthMethods = map[AuthMethod]bool{
	AuthAPIKey: true,
	AuthOAuth2: true,
	AuthMTLS:   true,
}

// FieldError reports a single invalid field. Field uses dot notation
// for nested structs (e.g. "rate_limit.burst") so callers can map
// errors back to specific form inputs or config keys.
type FieldError struct {
	Field   string
	Message string
}

func (e FieldError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ValidationErrors aggregates every FieldError found for a Config so
// callers see all problems in one pass instead of fixing issues one
// at a time across repeated submissions.
type ValidationErrors []FieldError

func (v ValidationErrors) Error() string {
	if len(v) == 0 {
		return "no validation errors"
	}
	msgs := make([]string, len(v))
	for i, e := range v {
		msgs[i] = e.Error()
	}
	return strings.Join(msgs, "; ")
}

// Validate checks every field of c and returns a non-nil error
// (concretely a ValidationErrors) describing all violations found, or
// nil if c is well-formed. Callers that need per-field detail should
// type-assert the returned error to ValidationErrors.
func (c Config) Validate() error {
	var errs ValidationErrors

	if c.ID == "" {
		errs = append(errs, FieldError{"id", "must not be empty"})
	} else if !idPattern.MatchString(c.ID) {
		errs = append(errs, FieldError{"id", "must be 3-63 lowercase alphanumeric characters or hyphens, and must start/end with an alphanumeric"})
	}

	if strings.TrimSpace(c.DisplayName) == "" {
		errs = append(errs, FieldError{"display_name", "must not be empty"})
	} else if len(c.DisplayName) > 128 {
		errs = append(errs, FieldError{"display_name", "must be 128 characters or fewer"})
	}

	if !validAuthMethods[c.Auth] {
		errs = append(errs, FieldError{"auth", fmt.Sprintf("must be one of api_key, oauth2, mtls (got %q)", c.Auth)})
	}

	if c.DataSchemaVersion == "" {
		errs = append(errs, FieldError{"data_schema_version", "must not be empty"})
	} else if !dataSchemaVersionPattern.MatchString(c.DataSchemaVersion) {
		errs = append(errs, FieldError{"data_schema_version", fmt.Sprintf("must match vN or vN.N (got %q)", c.DataSchemaVersion)})
	}

	if c.RateLimit.RequestsPerSecond <= 0 {
		errs = append(errs, FieldError{"rate_limit.requests_per_second", "must be greater than 0"})
	}
	if c.RateLimit.Burst < c.RateLimit.RequestsPerSecond {
		errs = append(errs, FieldError{"rate_limit.burst", "must be greater than or equal to requests_per_second"})
	}

	for flag := range c.FeatureFlags {
		if strings.TrimSpace(flag) == "" {
			errs = append(errs, FieldError{"feature_flags", "flag names must not be empty"})
			break
		}
	}

	if len(errs) == 0 {
		return nil
	}
	return errs
}
