package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/gzamboni/terraform-provider-er605/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// er605_setting manages a singleton form (one object, read with "get", written with "set").
type settingResource struct{ c *client.Client }

func newSettingResource() resource.Resource { return &settingResource{} }

type settingModel struct {
	ID       types.String `tfsdk:"id"`
	Endpoint types.String `tfsdk:"endpoint"`
	Config   types.String `tfsdk:"config"`
}

func (r *settingResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "er605_setting"
}

func (r *settingResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A singleton router form (DHCP server, time, DNS, ...). Only the keys given in config are managed and drift-checked; delete just forgets it (the router keeps the last values).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"endpoint": schema.StringAttribute{Required: true, Description: "e.g. /admin/dhcps?form=setting",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"config": schema.StringAttribute{Required: true, Description: "jsonencode({...}) of the fields to set, same names/values the read form returns."},
		},
	}
}

func (r *settingResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if req.ProviderData != nil {
		r.c = req.ProviderData.(*client.Client)
	}
}

func (r *settingResource) write(m *settingModel) error {
	var cfg map[string]any
	if err := json.Unmarshal([]byte(m.Config.ValueString()), &cfg); err != nil {
		return fmt.Errorf("config is not a JSON object: %w", err)
	}
	// The router rejects partial sets, so overlay config on the current full object.
	raw, err := r.c.Call(m.Endpoint.ValueString(), "get", nil)
	if err != nil {
		return err
	}
	full := map[string]any{}
	if err := json.Unmarshal(raw, &full); err != nil {
		return fmt.Errorf("not a singleton form (got %s)", raw)
	}
	for k, v := range cfg {
		full[k] = v
	}
	if _, err := r.c.Call(m.Endpoint.ValueString(), "set", full); err != nil {
		return err
	}
	m.ID = m.Endpoint
	return nil
}

func (r *settingResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m settingModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.write(&m); err != nil {
		resp.Diagnostics.AddError("Setting "+m.Endpoint.ValueString(), err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *settingResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	r.Create(ctx, resource.CreateRequest{Plan: req.Plan}, (*resource.CreateResponse)(resp))
}

func (r *settingResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m settingModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	raw, err := r.c.Call(m.Endpoint.ValueString(), "get", nil)
	if err != nil {
		resp.Diagnostics.AddError("Reading "+m.Endpoint.ValueString(), err.Error())
		return
	}
	var want, got map[string]any
	if json.Unmarshal([]byte(m.Config.ValueString()), &want) != nil || json.Unmarshal(raw, &got) != nil {
		resp.Diagnostics.AddError("Reading "+m.Endpoint.ValueString(), "unexpected response: "+string(raw))
		return
	}
	// Project the router state onto the managed keys; scalars that print the same ("1024" vs 1024) count as equal.
	proj := map[string]any{}
	for k, w := range want {
		if g, ok := got[k]; ok && fmt.Sprint(g) != fmt.Sprint(w) {
			proj[k] = g
		} else {
			proj[k] = w
		}
	}
	if !reflect.DeepEqual(proj, want) {
		b, _ := json.Marshal(proj)
		m.Config = types.StringValue(string(b))
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *settingResource) Delete(context.Context, resource.DeleteRequest, *resource.DeleteResponse) {}
