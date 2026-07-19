// Package tenant defines the tenant configuration domain model used
// throughout admitctl: registration, validation, and the schema that
// every onboarding path (CLI, API, batch import) must agree on.
package tenant

// AuthMethod identifies how a tenant's traffic authenticates against
// the platform. New methods must be added to the allow-list in
// validMethods (validation.go) or they will be rejected at onboarding.
type AuthMethod string

const (
	AuthAPIKey AuthMethod = "api_key"
	AuthOAuth2 AuthMethod = "oauth2"
	AuthMTLS   AuthMethod = "mtls"
)

// RateLimit describes the traffic ceiling enforced for a tenant.
// Burst must be >= RequestsPerSecond; that invariant is enforced in
// Validate, not here, so RateLimit stays a plain data holder.
type RateLimit struct {
	RequestsPerSecond int
	Burst             int
}

// Config is the full onboarding record for a single tenant. It is the
// unit that gets validated, persisted in the registry, and read back
// out by the health-check and rollback subsystems.
type Config struct {
	ID                string
	DisplayName       string
	Auth              AuthMethod
	DataSchemaVersion string
	RateLimit         RateLimit
	FeatureFlags      map[string]bool
}