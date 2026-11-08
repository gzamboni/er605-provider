package provider

import (
	"context"

	"github.com/gzamboni/terraform-provider-er605/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// er605_admin_account: the login used to manage the router (System > Administration > Account).
// The router never returns the password, so there is no drift detection: state is what Terraform last applied.
type accountResource struct{ c *client.Client }

func newAccountResource() resource.Resource { return &accountResource{} }

type accountModel struct {
	ID       types.String `tfsdk:"id"`
	Username types.String `tfsdk:"username"`
	Password types.String `tfsdk:"password"`
}

func (r *accountResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "er605_admin_account"
}

func (r *accountResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Router management account (the single admin login). Apply authenticates with the provider's current " +
			"credentials and replaces them; afterwards set the provider's username/password (or ER605_USERNAME/ER605_PASSWORD) " +
			"to the new values, or the next run cannot log in. Not readable back from the router: no drift detection. Destroy only forgets it.",
		Attributes: map[string]schema.Attribute{
			"id":       schema.StringAttribute{Computed: true},
			"username": schema.StringAttribute{Required: true, Description: "1-64 visible ASCII characters."},
			"password": schema.StringAttribute{Required: true, Sensitive: true, Description: "6-64 visible ASCII characters."},
		},
	}
}

func (r *accountResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if req.ProviderData != nil {
		r.c = req.ProviderData.(*client.Client)
	}
}

func (r *accountResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m accountModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.c.SetAccount(m.Username.ValueString(), m.Password.ValueString()); err != nil {
		resp.Diagnostics.AddError("Setting admin account", err.Error())
		return
	}
	m.ID = types.StringValue("admin")
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *accountResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	r.Create(ctx, resource.CreateRequest{Plan: req.Plan}, (*resource.CreateResponse)(resp))
}

func (r *accountResource) Read(context.Context, resource.ReadRequest, *resource.ReadResponse) {}

func (r *accountResource) Delete(context.Context, resource.DeleteRequest, *resource.DeleteResponse) {}
