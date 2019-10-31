package appregistry

import (
	"fmt"
	"io/ioutil"
	"path"
)

type AppregistryBuildOptions struct {
	Appender ImageAppender

	From, To            string
	AuthToken           string
	AppRegistryEndpoint string
	AppRegistryOrg      string
	DatabasePath        string
	CacheDir            string

	// derived
	CleanOutput bool
	ManifestDir string
	DatabaseDir string
}

func (o *AppregistryBuildOptions) Validate() error {
	// TODO: better validation

	if o.AppRegistryEndpoint == "" {
		return fmt.Errorf("app-registry must be a valid app-registry endpoint")
	}
	if o.AppRegistryOrg == "" {
		return fmt.Errorf("app-registry org (namespace) must be specified")
	}
	if o.Appender == nil {
		return fmt.Errorf("appregistry image builder can't run without an appender")
	}
	if o.From == "" {
		return fmt.Errorf("base image required (--from)")
	}
	if o.DatabasePath == "" {
		return fmt.Errorf("database must have a path to save to")
	}
	if o.ManifestDir == "" {
		return fmt.Errorf("local manifest directory required")
	}
	if o.DatabaseDir == "" {
		return fmt.Errorf("local database directory required")
	}

	return nil
}

func (o *AppregistryBuildOptions) Complete() error {
	// if a user has specified a specific cache dir, don't clean it after run
	o.CleanOutput = o.CacheDir == ""

	// build a separate path for manifests and the built database, so that
	// building is idempotent
	manifestDir, err := ioutil.TempDir("", "manifests-")
	if err != nil {
		return err
	}
	o.ManifestDir = manifestDir
	databaseDir, err := ioutil.TempDir("", "db-")
	if err != nil {
		return err
	}
	o.DatabaseDir = databaseDir

	if o.DatabasePath == "" {
		o.DatabasePath = path.Join(o.DatabaseDir, "bundles.db")
	}
	return nil
}

// Apply sequentially applies the given options to the config.
func (c *AppregistryBuildOptions) Apply(options []AppregistryBuildOption) {
	for _, option := range options {
		option(c)
	}
}

// ToOption converts an AppregistryBuildOptions object into a function that applies
// its current configuration to another AppregistryBuildOptions instance
func (c *AppregistryBuildOptions) ToOption() AppregistryBuildOption {
	return  func(o *AppregistryBuildOptions) {
		if c.Appender != nil {
			o.Appender = c.Appender
		}
		if c.From != "" {
			o.From = c.From
		}
		if c.To != "" {
			o.To = c.To
		}
		if c.AuthToken != "" {
			o.AuthToken = c.AuthToken
		}
		if c.AppRegistryEndpoint != "" {
			o.AppRegistryEndpoint = c.AppRegistryEndpoint
		}
		if c.DatabasePath != "" {
			o.DatabasePath = c.DatabasePath
		}
		if c.CacheDir != "" {
			o.CacheDir = c.CacheDir
		}
	}
}

type AppregistryBuildOption func(*AppregistryBuildOptions)

func DefaultAppregistryBuildOptions() *AppregistryBuildOptions {
	return &AppregistryBuildOptions{
		AppRegistryEndpoint: "https://quay.io/cnr",
		From:                "quay.io/operator-framework/operator-registry-server:latest",
	}
}

func WithAppender(a ImageAppender) AppregistryBuildOption {
	return func(o *AppregistryBuildOptions) {
		o.Appender = a
	}
}

func WithFrom(s string) AppregistryBuildOption {
	return func(o *AppregistryBuildOptions) {
		o.From = s
	}
}

func WithTo(s string) AppregistryBuildOption {
	return func(o *AppregistryBuildOptions) {
		o.To = s
	}
}

func WithAuthToken(s string) AppregistryBuildOption {
	return func(o *AppregistryBuildOptions) {
		o.AuthToken = s
	}
}
func WithAppRegistryEndpoint(s string) AppregistryBuildOption {
	return func(o *AppregistryBuildOptions) {
		o.AppRegistryEndpoint = s
	}
}

func WithAppRegistryOrg(s string) AppregistryBuildOption {
	return func(o *AppregistryBuildOptions) {
		o.AppRegistryOrg = s
	}
}

func WithDatabasePath(s string) AppregistryBuildOption {
	return func(o *AppregistryBuildOptions) {
		o.DatabasePath = s
	}
}

func WithCacheDir(s string) AppregistryBuildOption {
	return func(o *AppregistryBuildOptions) {
		o.CacheDir = s
	}
}
