// Command onboard-client is the CLI front-end for admitctl's tenant
// registry. It hydrates a Registry from a local JSON snapshot at
// startup, applies exactly one operation, persists the result, and
// exits — there is no long-running server here, by design: the CLI
// is meant to be scripted (CI pipelines, onboarding runbooks) as
// easily as it's run by hand.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/hassanli775/admitctl/internal/registry"
	"github.com/hassanli775/admitctl/internal/store"
	"github.com/hassanli775/admitctl/internal/tenant"
)

func main() {
	os.Exit(run(os.Args[1:], defaultStorePath(), os.Stdout, os.Stderr))
}

// run executes exactly one subcommand and returns a process exit
// code. It takes storePath, stdout, and stderr as parameters (rather
// than reaching for globals) specifically so tests can point it at a
// throwaway file and capture output without touching the real
// filesystem or terminal.
func run(args []string, storePath string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		printUsage(stderr)
		return 2
	}

	reg := registry.NewRegistry()
	records, err := store.Load(storePath)
	switch {
	case err == nil:
		reg.Restore(records)
	case errors.Is(err, store.ErrNotExist):
		// First run: nothing to hydrate, start empty.
	default:
		fmt.Fprintf(stderr, "admitctl: failed to load tenant store: %v\n", err)
		return 1
	}

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "register":
		return cmdRegister(reg, storePath, rest, stdout, stderr)
	case "list":
		return cmdList(reg, rest, stdout, stderr)
	case "get":
		return cmdGet(reg, rest, stdout, stderr)
	case "deactivate":
		return cmdDeactivate(reg, storePath, rest, stdout, stderr)
	case "-h", "--help", "help":
		printUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "admitctl: unknown command %q\n\n", cmd)
		printUsage(stderr)
		return 2
	}
}

func cmdRegister(reg *registry.Registry, storePath string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("register", flag.ContinueOnError)
	fs.SetOutput(stderr)
	id := fs.String("id", "", "tenant ID, e.g. acme-corp (required)")
	name := fs.String("name", "", "human-readable display name (required)")
	auth := fs.String("auth", "", "auth method: api_key, oauth2, or mtls (required)")
	schemaVersion := fs.String("schema-version", "v1", "data schema version, e.g. v1 or v1.2")
	rps := fs.Int("rps", 0, "requests per second (required, must be > 0)")
	burst := fs.Int("burst", 0, "burst capacity (defaults to --rps if unset)")
	flags := fs.String("flags", "", "comma-separated feature flags, e.g. beta_dashboard,new_ui")

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *burst == 0 {
		*burst = *rps
	}

	cfg := tenant.Config{
		ID:                *id,
		DisplayName:       *name,
		Auth:              tenant.AuthMethod(*auth),
		DataSchemaVersion: *schemaVersion,
		RateLimit:         tenant.RateLimit{RequestsPerSecond: *rps, Burst: *burst},
		FeatureFlags:      parseFeatureFlags(*flags),
	}

	if err := reg.Register(cfg); err != nil {
		var verrs tenant.ValidationErrors
		if errors.As(err, &verrs) {
			fmt.Fprintln(stderr, "admitctl: invalid tenant configuration:")
			for _, fe := range verrs {
				fmt.Fprintf(stderr, "  - %s\n", fe)
			}
			return 1
		}
		fmt.Fprintf(stderr, "admitctl: %v\n", err)
		return 1
	}

	if err := persist(reg, storePath); err != nil {
		fmt.Fprintf(stderr, "admitctl: tenant registered but failed to save: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "tenant %q registered (auth=%s, rps=%d, burst=%d)\n", *id, *auth, *rps, *burst)
	return 0
}

func cmdList(reg *registry.Registry, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	records := reg.List()
	if len(records) == 0 {
		fmt.Fprintln(stdout, "no tenants registered")
		return 0
	}

	tw := tabwriter.NewWriter(stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tSTATUS\tAUTH\tRPS\tBURST")
	for _, rec := range records {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%d\n",
			rec.Config.ID, rec.Status, rec.Config.Auth,
			rec.Config.RateLimit.RequestsPerSecond, rec.Config.RateLimit.Burst)
	}
	if err := tw.Flush(); err != nil {
		fmt.Fprintf(stderr, "admitctl: failed to render tenant list: %v\n", err)
		return 1
	}
	return 0
}

func cmdGet(reg *registry.Registry, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("get", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "admitctl: get requires exactly one tenant ID, e.g. `admitctl get acme-corp`")
		return 2
	}

	rec, err := reg.Get(fs.Arg(0))
	if err != nil {
		fmt.Fprintf(stderr, "admitctl: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "id:                  %s\n", rec.Config.ID)
	fmt.Fprintf(stdout, "display name:        %s\n", rec.Config.DisplayName)
	fmt.Fprintf(stdout, "status:              %s\n", rec.Status)
	fmt.Fprintf(stdout, "auth:                %s\n", rec.Config.Auth)
	fmt.Fprintf(stdout, "data schema version: %s\n", rec.Config.DataSchemaVersion)
	fmt.Fprintf(stdout, "rate limit:          %d req/s (burst %d)\n", rec.Config.RateLimit.RequestsPerSecond, rec.Config.RateLimit.Burst)
	if len(rec.Config.FeatureFlags) > 0 {
		flagNames := make([]string, 0, len(rec.Config.FeatureFlags))
		for name := range rec.Config.FeatureFlags {
			flagNames = append(flagNames, name)
		}
		fmt.Fprintf(stdout, "feature flags:       %s\n", strings.Join(flagNames, ", "))
	}
	fmt.Fprintf(stdout, "created at:          %s\n", rec.CreatedAt.Format("2006-01-02T15:04:05Z07:00"))
	fmt.Fprintf(stdout, "updated at:          %s\n", rec.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"))
	return 0
}

func cmdDeactivate(reg *registry.Registry, storePath string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("deactivate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "admitctl: deactivate requires exactly one tenant ID, e.g. `admitctl deactivate acme-corp`")
		return 2
	}

	id := fs.Arg(0)
	if err := reg.Deactivate(id); err != nil {
		fmt.Fprintf(stderr, "admitctl: %v\n", err)
		return 1
	}
	if err := persist(reg, storePath); err != nil {
		fmt.Fprintf(stderr, "admitctl: tenant deactivated but failed to save: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "tenant %q deactivated\n", id)
	return 0
}

func persist(reg *registry.Registry, storePath string) error {
	return store.Save(storePath, reg.List())
}

func parseFeatureFlags(raw string) map[string]bool {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	out := make(map[string]bool)
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out[part] = true
		}
	}
	return out
}

func defaultStorePath() string {
	if p := os.Getenv("ADMITCTL_STORE"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "admitctl-tenants.json"
	}
	return filepath.Join(home, ".admitctl", "tenants.json")
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `admitctl onboard-client — manage tenant onboarding

Usage:
  onboard-client register --id ID --name NAME --auth METHOD --rps N [--burst N] [--schema-version V] [--flags a,b,c]
  onboard-client list
  onboard-client get ID
  onboard-client deactivate ID

Environment:
  ADMITCTL_STORE   path to the tenant store file (default: ~/.admitctl/tenants.json)
`)
}