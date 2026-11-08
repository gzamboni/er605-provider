package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gzamboni/terraform-provider-er605/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type readDataSource struct{ c *client.Client }

func newReadDataSource() datasource.DataSource { return &readDataSource{} }

type readModel struct {
	Endpoint types.String `tfsdk:"endpoint"`
	Params   types.String `tfsdk:"params"`
	JSON     types.String `tfsdk:"json"`
}

func (d *readDataSource) Metadata(_ context.Context, _ datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = "er605_read"
}

func (d *readDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Reads any router form (\"get\"), e.g. /admin/dhcps?form=setting. Result is the raw JSON; use jsondecode().",
		Attributes: map[string]schema.Attribute{
			"endpoint": schema.StringAttribute{Required: true, Description: "Path + form, e.g. /admin/nat?form=vs"},
			"params":   schema.StringAttribute{Optional: true, Description: "Optional JSON object sent as params (e.g. {\"start\":0,\"end\":99} for paged lists)."},
			"json":     schema.StringAttribute{Computed: true, Description: "Raw \"result\" JSON."},
		},
	}
}

func (d *readDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, _ *datasource.ConfigureResponse) {
	if req.ProviderData != nil {
		d.c = req.ProviderData.(*client.Client)
	}
}

func (d *readDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m readModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var params any
	if !m.Params.IsNull() {
		if err := json.Unmarshal([]byte(m.Params.ValueString()), &params); err != nil {
			resp.Diagnostics.AddError("Invalid params JSON", err.Error())
			return
		}
	}
	raw, err := d.c.Call(m.Endpoint.ValueString(), "get", params)
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Reading %s", m.Endpoint.ValueString()), err.Error())
		return
	}
	m.JSON = types.StringValue(string(raw))
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
