package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// er605_vlan_port: link type and PVID of one switch port (Network > VLAN > Port setting). The router only accepts
// the whole port table in one set, so a write reads all ports, changes this one and writes the table back.
// Reads/import reuse the list engine (key = port number). Ports always exist: destroy only forgets.
type vlanPortResource struct{ typedResource }

var vlanPortSpec = &spec{
	typeName: "vlan_port",
	desc:     "Link type and PVID of one switch port (port 1 = WAN, 2-3 = WAN/LAN, 4-5 = LAN on the ER605). Destroy only stops managing the port. Changing a port you are connected through can cut your session.",
	endpoint: "/admin/vlansetting?form=portsettinglist",
	key:      "port",
	fields: []field{
		str("port").about("port number"), str("type").required().about("access, trunk or general"), str("pvid").required().about("native VLAN id"),
		{name: "vlans", k: kStrList, ro: true},
	},
}

func newVLANPortResource() resource.Resource {
	return &vlanPortResource{typedResource{s: vlanPortSpec}}
}

func (r *vlanPortResource) write(ctx context.Context, plan getter, st setter, d *diag.Diagnostics) {
	defer lockEndpoint(r.s.endpoint)()
	port := r.keyOf(ctx, plan, d)
	b := r.body(ctx, plan, d)
	if d.HasError() {
		return
	}
	rows, idx, err := r.locate(port)
	if err == nil && idx < 0 {
		err = fmt.Errorf("port %q does not exist", port)
	}
	if err == nil {
		table := make([]map[string]any, len(rows))
		for i, row := range rows {
			e := map[string]any{"port": row["port"], "type": row["type"], "pvid": row["pvid"]}
			if i == idx {
				e["type"], e["pvid"] = b["type"], b["pvid"]
			}
			table[i] = e
		}
		_, err = r.c.Call(r.s.endpoint, "set", table)
	}
	if err == nil {
		rows, idx, err = r.locate(port)
		if err == nil && idx < 0 {
			err = fmt.Errorf("port %q vanished", port)
		}
	}
	if err != nil {
		d.AddError("Writing port "+port, err.Error())
		return
	}
	r.fill(ctx, st, rows[idx], port, d)
}

func (r *vlanPortResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	r.write(ctx, req.Plan, &resp.State, &resp.Diagnostics)
}

func (r *vlanPortResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	r.write(ctx, req.Plan, &resp.State, &resp.Diagnostics)
}

func (r *vlanPortResource) Delete(context.Context, resource.DeleteRequest, *resource.DeleteResponse) {
}
