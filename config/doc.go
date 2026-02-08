// Package config manages environment configuration, authentication, and runtime settings.
// Environments are loaded from YAML files and provide API base URLs, auth credentials
// (oauth2, apikey, bearer), LLM configuration, and runtime settings. Secrets can be
// resolved from environment variables or literal values via SecretRef.
package config
