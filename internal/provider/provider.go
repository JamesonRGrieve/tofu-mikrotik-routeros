// SPDX-License-Identifier: AGPL-3.0-or-later

// Package provider implements the mikrotik-routeros OpenTofu/Terraform provider
// — a native client for the MikroTik RouterOS v7+ REST API. It is generic over
// the API surface (the routeros_object resource/data source address any /rest
// menu path), giving 100% feature coverage without per-feature code.
package provider

import (
	"context"

	"github.com/JamesonRGrieve/tofu-mikrotik-routeros/internal/routeros"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ provider.Provider = (*routerosProvider)(nil)

// New returns the provider factory for a given version.
func New(version string) func() provider.Provider {
	return func() provider.Provider { return &routerosProvider{version: version} }
}

type routerosProvider struct {
	version string
}

type providerModel struct {
	Host     types.String `tfsdk:"host"`
	Username types.String `tfsdk:"username"`
	Password types.String `tfsdk:"password"`
	Insecure types.Bool   `tfsdk:"insecure"`
	Scheme   types.String `tfsdk:"scheme"`
}

func (p *routerosProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	// Single-token type name -> resources are `routeros_object`, so Terraform's
	// prefix-before-first-underscore inference resolves the local name cleanly
	// (the source address is still jamesonrgrieve/mikrotik-routeros).
	resp.TypeName = "routeros"
	resp.Version = p.version
}

func (p *routerosProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Native provider for MikroTik RouterOS v7+ routers via the REST API " +
			"(`/rest`, HTTP Basic auth). Generic over the menu tree — `routeros_object` addresses any path.",
		Attributes: map[string]schema.Attribute{
			"host": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Router address (host or host:port), no scheme.",
			},
			"username": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "RouterOS username.",
			},
			"password": schema.StringAttribute{
				Required:            true,
				Sensitive:           true,
				MarkdownDescription: "RouterOS password.",
			},
			"insecure": schema.BoolAttribute{
				Optional: true,
				MarkdownDescription: "Skip TLS verification (default true — RouterOS ships a self-signed cert). " +
					"Set false only with a trusted cert installed.",
			},
			"scheme": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "`https` (default, `www-ssl` service) or `http` (`www` service, RouterOS v7.9+).",
			},
		},
	}
}

func (p *routerosProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	insecure := true
	if !cfg.Insecure.IsNull() && !cfg.Insecure.IsUnknown() {
		insecure = cfg.Insecure.ValueBool()
	}
	scheme := ""
	if !cfg.Scheme.IsNull() && !cfg.Scheme.IsUnknown() {
		scheme = cfg.Scheme.ValueString()
	}
	client := routeros.NewClient(routeros.Config{
		Host:     cfg.Host.ValueString(),
		Username: cfg.Username.ValueString(),
		Password: cfg.Password.ValueString(),
		Insecure: insecure,
		Scheme:   scheme,
	})
	resp.ResourceData = client
	resp.DataSourceData = client
}

func (p *routerosProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{NewObjectResource}
}

func (p *routerosProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{NewObjectDataSource}
}
