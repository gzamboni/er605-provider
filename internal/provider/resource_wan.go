package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// er605_wan: Network > WAN > Connection configuration of one WAN port. Unlike the other typed resources the read
// needs {wan_id, proto} as parameters and the set of fields depends on proto, so it has its own CRUD on top of the
// typedResource schema/value helpers.
type wanResource struct{ typedResource }

const wanEndpoint = "/admin/interface_wan?form=wanconfig"

var wanSpec = &spec{
	typeName: "wan",
	desc: "Network > WAN connection of one port (wan_id 1 = WAN, 2 = WAN/LAN1, ...). proto selects which of the other attributes apply: dhcp (hostname, unicast), static (ipaddr, netmask, gateway), pppoe (username, password, service, mru). " +
		"Changing a WAN re-dials it: expect a few seconds of outage on that link. Destroy only stops managing the port.",
	endpoint: wanEndpoint,
	drop:     []string{"vlantable", "bandwidth", "rate", "maxpppoemtu"},
	fields: []field{
		{name: "wan_id", k: kStr, req: true, replace: true},
		str("proto").required().about("dhcp, static or pppoe"),
		num("uplink").about("kbps"), num("downlink").about("kbps"), num("mtu"),
		flag("vlan_tag").rd("vlanTag").wr("vlanTag").words("1", "0"), str("vlanid"),
		str("dns1"), str("dns2"),
		str("hostname"), flag("unicast"),
		str("ipaddr"), str("netmask"), str("gateway"),
		str("username"), field{name: "password", k: kStr, secret: true}, str("service"), num("mru"),
	},
}

func newWANResource() resource.Resource { return &wanResource{typedResource{s: wanSpec}} }

func (r *wanResource) get(wanID, proto string) (map[string]any, error) {
	raw, err := r.c.Call(wanEndpoint, "get", map[string]any{"wan_id": wanID, "proto": proto})
	if err != nil {
		return nil, err
	}
	m := map[string]any{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("unexpected response: %s", raw)
	}
	return m, nil
}

func (r *wanResource) ids(ctx context.Context, g getter, d *diag.Diagnostics) (string, string) {
	var wanID, proto string
	d.Append(g.GetAttribute(ctx, path.Root("wan_id"), &wanID)...)
	d.Append(g.GetAttribute(ctx, path.Root("proto"), &proto)...)
	return wanID, proto
}

func (r *wanResource) write(ctx context.Context, plan getter, st setter, d *diag.Diagnostics) {
	wanID, proto := r.ids(ctx, plan, d)
	b := r.body(ctx, plan, d)
	if d.HasError() {
		return
	}
	cur, err := r.get(wanID, proto)
	if err == nil {
		for _, k := range r.s.drop {
			delete(cur, k)
		}
		for k, v := range b {
			cur[k] = v
		}
		_, err = r.c.Call(wanEndpoint, "set", cur)
	}
	if err == nil {
		cur, err = r.get(wanID, proto)
	}
	if err != nil {
		d.AddError("Writing WAN "+wanID, err.Error())
		return
	}
	r.fill(ctx, st, cur, "WAN"+wanID, d)
	d.Append(st.SetAttribute(ctx, path.Root("wan_id"), wanID)...)
}

func (r *wanResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	r.write(ctx, req.Plan, &resp.State, &resp.Diagnostics)
}

func (r *wanResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	r.write(ctx, req.Plan, &resp.State, &resp.Diagnostics)
}

func (r *wanResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	wanID, proto := r.ids(ctx, req.State, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	cur, err := r.get(wanID, proto)
	if err != nil {
		resp.Diagnostics.AddError("Reading WAN "+wanID, err.Error())
		return
	}
	r.fill(ctx, &resp.State, cur, "WAN"+wanID, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("wan_id"), wanID)...)
}

// ImportState takes the wan_id ("1", "2", ...) and looks the configured proto up in the interface status.
func (r *wanResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	raw, err := r.c.Call("/admin/interface?form=status3", "get", nil)
	var st struct {
		Normal []struct {
			Name   string `json:"t_name"`
			Proto  string `json:"t_proto"`
			Second bool   `json:"second_conn"`
		} `json:"normal"`
	}
	if err == nil {
		err = json.Unmarshal(raw, &st)
	}
	if err != nil {
		resp.Diagnostics.AddError("Importing WAN", err.Error())
		return
	}
	for _, i := range st.Normal {
		if i.Name == "WAN"+req.ID && !i.Second {
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("wan_id"), req.ID)...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("proto"), i.Proto)...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), "WAN"+req.ID)...)
			return
		}
	}
	resp.Diagnostics.AddError("Importing WAN", fmt.Sprintf("no WAN%s in the interface status", req.ID))
}

func (r *wanResource) Delete(context.Context, resource.DeleteRequest, *resource.DeleteResponse) {}
