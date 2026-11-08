package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gzamboni/terraform-provider-er605/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// er605_list_item manages one row of a list form (virtual servers, static routes, DHCP reservations, ...).
// The router addresses rows by position, so rows are located by a key field on every operation.
type listItemResource struct{ c *client.Client }

func newListItemResource() resource.Resource { return &listItemResource{} }

type listItemModel struct {
	ID           types.String `tfsdk:"id"`
	Endpoint     types.String `tfsdk:"endpoint"`
	KeyField     types.String `tfsdk:"key_field"`
	ReadKeyField types.String `tfsdk:"read_key_field"`
	Item         types.String `tfsdk:"item"`
}

func (r *listItemResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "er605_list_item"
}

func (r *listItemResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "One row of a router list form. Existence is tracked; field drift is not (write and read shapes differ on some forms).",
		Attributes: map[string]schema.Attribute{
			"id":             schema.StringAttribute{Computed: true},
			"endpoint":       schema.StringAttribute{Required: true, Description: "e.g. /admin/nat?form=vs", PlanModifiers: replace},
			"key_field":      schema.StringAttribute{Required: true, Description: "Field in item that uniquely identifies the row (e.g. name, mac).", PlanModifiers: replace},
			"read_key_field": schema.StringAttribute{Optional: true, Description: "Field the read form uses for the key when it differs from key_field (e.g. ipaddr -> ip). Defaults to key_field."},
			"item":           schema.StringAttribute{Required: true, Description: "jsonencode({...}) row body exactly as the web UI submits it."},
		},
	}
}

func (r *listItemResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if req.ProviderData != nil {
		r.c = req.ProviderData.(*client.Client)
	}
}

func (m *listItemModel) readKey() string {
	if !m.ReadKeyField.IsNull() && m.ReadKeyField.ValueString() != "" {
		return m.ReadKeyField.ValueString()
	}
	return m.KeyField.ValueString()
}

func (m *listItemModel) body() (map[string]any, string, error) {
	var item map[string]any
	if err := json.Unmarshal([]byte(m.Item.ValueString()), &item); err != nil {
		return nil, "", fmt.Errorf("item is not a JSON object: %w", err)
	}
	k, ok := item[m.KeyField.ValueString()]
	if !ok {
		return nil, "", fmt.Errorf("item has no %q field", m.KeyField.ValueString())
	}
	return item, fmt.Sprint(k), nil
}

// locate returns the row list and the position of the row whose read key equals key (-1 if absent).
func (r *listItemResource) locate(m *listItemModel, key string) ([]map[string]any, int, error) {
	raw, err := r.c.Call(m.Endpoint.ValueString(), "get", nil)
	if err != nil {
		return nil, -1, err
	}
	var rows []map[string]any // an empty list comes back as {}, which fails to unmarshal into a slice
	if json.Unmarshal(raw, &rows) != nil {
		rows = nil
	}
	for i, row := range rows {
		if fmt.Sprint(row[m.readKey()]) == key {
			return rows, i, nil
		}
	}
	return rows, -1, nil
}

func (r *listItemResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m listItemModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	defer lockEndpoint(m.Endpoint.ValueString())()
	item, key, err := m.body()
	if err == nil {
		var rows []map[string]any
		var idx int
		if rows, idx, err = r.locate(&m, key); err == nil && idx >= 0 {
			err = fmt.Errorf("row with %s=%q already exists; import is not supported, remove it from the router first", m.readKey(), key)
		} else if err == nil {
			_, err = r.c.Call(m.Endpoint.ValueString(), "add", map[string]any{"index": len(rows), "old": "add", "new": item})
		}
	}
	if err != nil {
		resp.Diagnostics.AddError("Creating row in "+m.Endpoint.ValueString(), err.Error())
		return
	}
	m.ID = types.StringValue(m.Endpoint.ValueString() + "|" + key)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *listItemResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m listItemModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	_, key, err := m.body()
	if err == nil {
		var idx int
		if _, idx, err = r.locate(&m, key); err == nil && idx < 0 {
			resp.State.RemoveResource(ctx)
			return
		}
	}
	if err != nil {
		resp.Diagnostics.AddError("Reading "+m.Endpoint.ValueString(), err.Error())
	}
}

func (r *listItemResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state listItemModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	defer lockEndpoint(plan.Endpoint.ValueString())()
	item, key, err := plan.body()
	if err == nil {
		var oldKey string
		if _, oldKey, err = state.body(); err == nil {
			var rows []map[string]any
			var idx int
			if rows, idx, err = r.locate(&state, oldKey); err == nil && idx < 0 {
				err = fmt.Errorf("row %s=%q no longer exists on the router; refresh and re-apply", state.readKey(), oldKey)
			} else if err == nil {
				_, err = r.c.Call(plan.Endpoint.ValueString(), "set", map[string]any{"index": idx, "old": rows[idx], "new": item, "key": fmt.Sprintf("key-%d", idx)})
			}
		}
	}
	if err != nil {
		resp.Diagnostics.AddError("Updating row in "+plan.Endpoint.ValueString(), err.Error())
		return
	}
	plan.ID = types.StringValue(plan.Endpoint.ValueString() + "|" + key)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *listItemResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m listItemModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	defer lockEndpoint(m.Endpoint.ValueString())()
	_, key, err := m.body()
	if err == nil {
		var idx int
		if _, idx, err = r.locate(&m, key); err == nil && idx >= 0 {
			_, err = r.c.Call(m.Endpoint.ValueString(), "delete", map[string]any{"key": fmt.Sprintf("key-%d", idx), "index": fmt.Sprint(idx)})
		}
	}
	if err != nil {
		resp.Diagnostics.AddError("Deleting row in "+m.Endpoint.ValueString(), err.Error())
	}
}
