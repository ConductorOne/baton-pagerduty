package config

import (
	"github.com/conductorone/baton-sdk/pkg/field"
)

var (
	Token = field.StringField(
		"token",
		field.WithDescription("The PagerDuty access token used to connect to the PagerDuty API. ($BATON_TOKEN)"),
		field.WithRequired(true),
		field.WithIsSecret(true),
	)
	BaseURL = field.StringField(
		"base-url",
		field.WithDescription("Override the PagerDuty API URL (for testing)"),
		field.WithHidden(true),
		field.WithExportTarget(field.ExportTargetCLIOnly),
	)

	// FieldRelationships defines relationships between the fields listed in
	// Config that can be automatically validated.
	FieldRelationships = []field.SchemaFieldRelationship{}
)

//go:generate go run ./gen
var Config = field.NewConfiguration(
	[]field.SchemaField{
		Token,
		BaseURL,
	},
	field.WithConnectorDisplayName("PagerDuty"),
	field.WithHelpUrl("/docs/baton/pagerduty"),
	field.WithIconUrl("/static/app-icons/pagerduty.svg"),
)

// ValidateConfig is run after the configuration is loaded, and should return an
// error if it isn't valid. Implementing this function is optional, it only
// needs to perform extra validations that cannot be encoded with configuration
// parameters.
func ValidateConfig(cfg *Pagerduty) error {
	return nil
}
