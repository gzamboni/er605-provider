package provider

import (
	"context"
	"os"
	"time"

	"github.com/gzamboni/terraform-provider-er605/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type er605Provider struct{}

func New() provider.Provider { return &er605Provider{} }

type providerModel struct {
	URL      types.String `tfsdk:"url"`
	Username types.String `tfsdk:"username"`
	Password types.String `tfsdk:"password"`
	Insecure types.Bool   `tfsdk:"insecure"`
	Timeout  types.Int64  `tfsdk:"timeout"`
}

func (p *er605Provider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "er605"
}

func (p *er605Provider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "TP-Link ER605 (standalone mode) via the router's web API.",
		Attributes: map[string]schema.Attribute{
			"url":      schema.StringAttribute{Optional: true, Description: "Router base URL. Default http://192.168.0.1 (env ER605_URL). The router's HTTPS only speaks legacy TLS; prefer http on the LAN."},
			"username": schema.StringAttribute{Optional: true, Description: "Admin user (env ER605_USERNAME)."},
			"password": schema.StringAttribute{Optional: true, Sensitive: true, Description: "Admin password (env ER605_PASSWORD)."},
			"insecure": schema.BoolAttribute{Optional: true, Description: "Skip TLS verification when url is https."},
			"timeout":  schema.Int64Attribute{Optional: true, Description: "Per-request timeout in seconds. Default 60; the router can take 5-35 s to apply a change."},
		},
	}
}

func (p *er605Provider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var m providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	pick := func(v types.String, env, def string) string {
		if !v.IsNull() && !v.IsUnknown() {
			return v.ValueString()
		}
		if e := os.Getenv(env); e != "" {
			return e
		}
		return def
	}
	user, pass := pick(m.Username, "ER605_USERNAME", ""), pick(m.Password, "ER605_PASSWORD", "")
	if user == "" || pass == "" {
		resp.Diagnostics.AddError("Missing credentials", "Set username/password or ER605_USERNAME/ER605_PASSWORD.")
		return
	}
	c := client.New(pick(m.URL, "ER605_URL", "http://192.168.0.1"), user, pass, m.Insecure.ValueBool())
	if !m.Timeout.IsNull() && !m.Timeout.IsUnknown() {
		c.SetTimeout(time.Duration(m.Timeout.ValueInt64()) * time.Second)
	}
	resp.DataSourceData, resp.ResourceData = c, c
}

func (p *er605Provider) DataSources(context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{newReadDataSource}
}

func (p *er605Provider) Resources(context.Context) []func() resource.Resource {
	rs := []func() resource.Resource{newSettingResource, newListItemResource, newWANResource, newAccountResource, newVLANPortResource}
	for _, s := range specs {
		rs = append(rs, typed(s))
	}
	return rs
}
