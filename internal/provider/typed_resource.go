package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/gzamboni/terraform-provider-er605/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// A typed resource is a table-driven view of one router form: a spec lists the fields (Terraform name, name the
// router reads/writes them under, type) and this engine provides schema, CRUD and import for it.

type kind int

const (
	kStr kind = iota
	kBool
	kInt     // sent to the router as a string, as its UI does
	kStrList // list of strings
)

type field struct {
	name    string // Terraform attribute
	k       kind
	read    string    // router name when reading, if different from name
	wire    string    // router name when writing, if different from name
	req     bool      // Required in Terraform
	ro      bool      // computed only, never written
	def     *bool     // default for bool fields
	bools   [2]string // router spellings of true/false (default on/off)
	desc    string
	secret  bool // Sensitive in Terraform
	replace bool // changing it forces a new resource
}

func (f field) readName() string {
	if f.read != "" {
		return f.read
	}
	return f.name
}

func (f field) wireName() string {
	if f.wire != "" {
		return f.wire
	}
	return f.name
}

func (f field) boolWords() (string, string) {
	if f.bools[0] != "" {
		return f.bools[0], f.bools[1]
	}
	return "on", "off"
}

type spec struct {
	typeName string // without provider prefix
	desc     string
	endpoint string
	key      string         // list forms: the field identifying a row; empty for singleton forms
	drop     []string       // singleton forms: read-only keys removed before set (router rejects them or they change state)
	rowKey   string         // list forms: row field the UI grid uses as key (default: positional "key-N")
	rowExtra string         // list forms: row field the UI grid sends as extraKey
	position string         // list forms: router name of a required 1-based position field (appended when unset)
	omit     []string       // list forms: row keys the UI never sends back on set (rejected by the router)
	consts   map[string]any // list forms: hidden constants the UI adds to every new row
	fields   []field
}

// rowParams addresses an existing row the way the UI's grid does: by position, plus the row's key value
// ("key-N" unless the screen's grid keys on a field) and, on some screens, the extra property.
func (r *typedResource) rowParams(row map[string]any, idx int) map[string]any {
	p := map[string]any{"index": fmt.Sprint(idx), "key": fmt.Sprintf("key-%d", idx)}
	if r.s.rowKey != "" {
		p["key"] = fmt.Sprint(row[r.s.rowKey])
		p[r.s.rowKey] = p["key"] // some screens (LAN) expect the key under the grid's key property name
	}
	if r.s.rowExtra != "" {
		p["extraKey"] = fmt.Sprint(row[r.s.rowExtra])
	}
	return p
}

func (s *spec) isList() bool { return s.key != "" }

func (s *spec) keyField() field {
	for _, f := range s.fields {
		if f.name == s.key {
			return f
		}
	}
	panic("spec " + s.typeName + ": key field missing")
}

type typedResource struct {
	s *spec
	c *client.Client
}

func typed(s *spec) func() resource.Resource {
	return func() resource.Resource { return &typedResource{s: s} }
}

func (r *typedResource) Metadata(_ context.Context, _ resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = "er605_" + r.s.typeName
}

func (r *typedResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if req.ProviderData != nil {
		r.c = req.ProviderData.(*client.Client)
	}
}

func (r *typedResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	attrs := map[string]schema.Attribute{"id": schema.StringAttribute{Computed: true}}
	if !r.s.isList() { // a list row's id is its key, which may change on update
		attrs["id"] = schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}}
	}
	for _, f := range r.s.fields {
		req, opt := f.req || f.name == r.s.key, !f.ro
		keep := !f.req && f.name != r.s.key
		switch f.k {
		case kStr:
			a := schema.StringAttribute{Description: f.desc, Required: req, Optional: opt && !req, Computed: !req, Sensitive: f.secret}
			if keep {
				a.PlanModifiers = []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
			}
			if f.replace {
				a.PlanModifiers = []planmodifier.String{stringplanmodifier.RequiresReplace()}
			}
			attrs[f.name] = a
		case kBool:
			a := schema.BoolAttribute{Description: f.desc, Required: req, Optional: opt && !req, Computed: !req}
			if f.def != nil && !req {
				a.Default = booldefault.StaticBool(*f.def)
			} else if keep {
				a.PlanModifiers = []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()}
			}
			attrs[f.name] = a
		case kInt:
			a := schema.Int64Attribute{Description: f.desc, Required: req, Optional: opt && !req, Computed: !req}
			if keep {
				a.PlanModifiers = []planmodifier.Int64{int64planmodifier.UseStateForUnknown()}
			}
			attrs[f.name] = a
		case kStrList:
			a := schema.ListAttribute{Description: f.desc, ElementType: types.StringType, Required: req, Optional: opt && !req, Computed: !req}
			if keep {
				a.PlanModifiers = []planmodifier.List{listplanmodifier.UseStateForUnknown()}
			}
			attrs[f.name] = a
		}
	}
	resp.Schema = schema.Schema{Description: r.s.desc, Attributes: attrs}
}

type getter interface {
	GetAttribute(context.Context, path.Path, any) diag.Diagnostics
}

// wireValue converts the Terraform value of f into what the router expects; ok is false when null/unknown.
func wireValue(ctx context.Context, g getter, f field, d *diag.Diagnostics) (any, bool) {
	p := path.Root(f.name)
	switch f.k {
	case kBool:
		var v types.Bool
		d.Append(g.GetAttribute(ctx, p, &v)...)
		if v.IsNull() || v.IsUnknown() {
			return nil, false
		}
		on, off := f.boolWords()
		if v.ValueBool() {
			return on, true
		}
		return off, true
	case kInt:
		var v types.Int64
		d.Append(g.GetAttribute(ctx, p, &v)...)
		if v.IsNull() || v.IsUnknown() {
			return nil, false
		}
		return strconv.FormatInt(v.ValueInt64(), 10), true
	case kStrList:
		var v types.List
		d.Append(g.GetAttribute(ctx, p, &v)...)
		if v.IsNull() || v.IsUnknown() {
			return nil, false
		}
		out := []string{}
		d.Append(v.ElementsAs(ctx, &out, false)...)
		return out, true
	}
	var v types.String
	d.Append(g.GetAttribute(ctx, p, &v)...)
	if v.IsNull() || v.IsUnknown() {
		return nil, false
	}
	return v.ValueString(), true
}

// tfValue converts the router value of f (absent -> null) into a Terraform value.
func tfValue(f field, row map[string]any) attr.Value {
	v, ok := row[f.readName()]
	switch f.k {
	case kBool:
		if !ok {
			return types.BoolNull()
		}
		on, _ := f.boolWords()
		return types.BoolValue(fmt.Sprint(v) == on)
	case kInt:
		n, err := strconv.ParseInt(fmt.Sprint(v), 10, 64)
		if !ok || err != nil {
			return types.Int64Null()
		}
		return types.Int64Value(n)
	case kStrList:
		els := []attr.Value{}
		a, isArr := v.([]any) // an empty list comes back as {}; a single member as a bare string
		if s, isStr := v.(string); isStr {
			a, isArr = []any{s}, true
		}
		if isArr {
			for _, e := range a {
				if s := fmt.Sprint(e); s != "" { // the router stores an empty selection as [""]
					els = append(els, types.StringValue(s))
				}
			}
		}
		return types.ListValueMust(types.StringType, els)
	}
	if !ok {
		return types.StringNull()
	}
	return types.StringValue(fmt.Sprint(v))
}

// body collects the writable planned values by router write name.
func (r *typedResource) body(ctx context.Context, g getter, d *diag.Diagnostics) map[string]any {
	b := map[string]any{}
	for _, f := range r.s.fields {
		if f.ro {
			continue
		}
		if v, ok := wireValue(ctx, g, f, d); ok {
			b[f.wireName()] = v
		}
	}
	return b
}

type setter interface {
	SetAttribute(context.Context, path.Path, any) diag.Diagnostics
}

func (r *typedResource) fill(ctx context.Context, st setter, row map[string]any, id string, d *diag.Diagnostics) {
	d.Append(st.SetAttribute(ctx, path.Root("id"), id)...)
	for _, f := range r.s.fields {
		d.Append(st.SetAttribute(ctx, path.Root(f.name), tfValue(f, row))...)
	}
}

func (r *typedResource) fetch() (json.RawMessage, error) {
	return r.c.Call(r.s.endpoint, "get", nil)
}

// rows returns the list form's rows and the position of the one whose key equals key (-1 if absent).
func (r *typedResource) locate(key string) ([]map[string]any, int, error) {
	raw, err := r.fetch()
	if err != nil {
		return nil, -1, err
	}
	var rows []map[string]any // an empty list comes back as {}, a single-row list as a bare object
	if json.Unmarshal(raw, &rows) != nil {
		rows = nil
		var one map[string]any
		if json.Unmarshal(raw, &one) == nil && len(one) > 0 {
			rows = []map[string]any{one}
		}
	}
	kn := r.s.keyField().readName()
	for i, row := range rows {
		if fmt.Sprint(row[kn]) == key {
			return rows, i, nil
		}
	}
	return rows, -1, nil
}

func (r *typedResource) single() (map[string]any, error) {
	raw, err := r.fetch()
	if err != nil {
		return nil, err
	}
	m := map[string]any{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("unexpected response: %s", raw)
	}
	return m, nil
}

func (r *typedResource) keyOf(ctx context.Context, g getter, d *diag.Diagnostics) string {
	var k types.String
	d.Append(g.GetAttribute(ctx, path.Root(r.s.key), &k)...)
	return k.ValueString()
}

// epLocks serialises read-modify-write sequences per form: Terraform applies resources in parallel and a list row is
// addressed by position, so two concurrent deletes/adds on one list would act on stale indexes.
var epLocks sync.Map

func lockEndpoint(ep string) (unlock func()) {
	m, _ := epLocks.LoadOrStore(ep, &sync.Mutex{})
	m.(*sync.Mutex).Lock()
	return m.(*sync.Mutex).Unlock
}

func (r *typedResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	defer lockEndpoint(r.s.endpoint)()
	b := r.body(ctx, req.Plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	row, id, err := r.apply(ctx, req.Plan, "", b, &resp.Diagnostics)
	if err != nil {
		resp.Diagnostics.AddError("Creating "+r.s.typeName, err.Error())
		return
	}
	r.fill(ctx, &resp.State, row, id, &resp.Diagnostics)
}

func (r *typedResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	defer lockEndpoint(r.s.endpoint)()
	b := r.body(ctx, req.Plan, &resp.Diagnostics)
	oldKey := ""
	if r.s.isList() {
		oldKey = r.keyOf(ctx, req.State, &resp.Diagnostics)
	}
	if resp.Diagnostics.HasError() {
		return
	}
	row, id, err := r.apply(ctx, req.Plan, oldKey, b, &resp.Diagnostics)
	if err != nil {
		resp.Diagnostics.AddError("Updating "+r.s.typeName, err.Error())
		return
	}
	r.fill(ctx, &resp.State, row, id, &resp.Diagnostics)
}

// apply writes b and returns the row as the router now reports it. oldKey is the existing row's key on update, "" on create.
func (r *typedResource) apply(ctx context.Context, plan getter, oldKey string, b map[string]any, d *diag.Diagnostics) (map[string]any, string, error) {
	if !r.s.isList() {
		// The router rejects partial sets, so overlay the plan on the current full object.
		full, err := r.single()
		if err != nil {
			return nil, "", err
		}
		for _, k := range r.s.drop {
			delete(full, k)
		}
		for k, v := range b {
			if l, ok := v.([]string); ok && len(l) == 0 {
				v = "" // the router rejects [] and a missing key; the web UI's empty selection is ""
			}
			full[k] = v
		}
		if _, err := r.c.Call(r.s.endpoint, "set", full); err != nil {
			return nil, "", err
		}
		row, err := r.single()
		return row, r.s.endpoint, err
	}
	key := r.keyOf(ctx, plan, d)
	rows, idx, err := r.locate(oldKey)
	if oldKey == "" {
		if err == nil && idx >= 0 {
			err = fmt.Errorf("%s=%q already exists on the router; use `terraform import` to adopt it", r.s.key, key)
		} else if err == nil {
			if p := r.s.position; p != "" {
				if _, set := b[p]; !set { // the router requires a 1-based position; default to the end
					b[p] = strconv.Itoa(len(rows) + 1)
				}
			}
			for k, v := range r.s.consts {
				b[k] = v
			}
			_, err = r.c.Call(r.s.endpoint, "add", map[string]any{"index": len(rows), "old": "add", "new": b})
		}
	} else if err == nil && idx < 0 {
		err = fmt.Errorf("%s=%q no longer exists on the router; refresh and re-apply", r.s.key, oldKey)
	} else if err == nil {
		// The UI's edit sends the whole row under the names the grid reads (add uses the write names): start from
		// the existing row so unset fields keep their values, then overlay the plan.
		nb := map[string]any{}
		for k, v := range rows[idx] {
			if !strings.HasPrefix(k, ".") { // uci metadata (.name, .type, ...) is read-only
				nb[k] = v
			}
		}
		for _, f := range r.s.fields {
			if v, ok := wireValue(ctx, plan, f, d); ok && !f.ro {
				nb[f.readName()] = v
			}
		}
		old := map[string]any{}
		for k, v := range rows[idx] {
			old[k] = v
		}
		for _, k := range r.s.omit {
			delete(old, k)
			delete(nb, k)
		}
		for k, v := range nb { // the router reports an empty list as {} but wants [] back
			if m, ok := v.(map[string]any); ok && len(m) == 0 {
				nb[k] = []any{}
			}
		}
		p := r.rowParams(rows[idx], idx)
		p["old"], p["new"], p["index"] = old, nb, idx
		_, err = r.c.Call(r.s.endpoint, "set", p)
	}
	if err != nil {
		return nil, "", err
	}
	rows, idx, err = r.locate(key)
	if err == nil && idx < 0 {
		err = fmt.Errorf("row %s=%q not found after write", r.s.key, key)
	}
	if err != nil {
		return nil, "", err
	}
	return rows[idx], key, nil
}

func (r *typedResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if !r.s.isList() {
		row, err := r.single()
		if err != nil {
			resp.Diagnostics.AddError("Reading "+r.s.typeName, err.Error())
			return
		}
		r.fill(ctx, &resp.State, row, r.s.endpoint, &resp.Diagnostics)
		return
	}
	key := r.keyOf(ctx, req.State, &resp.Diagnostics)
	rows, idx, err := r.locate(key)
	if err != nil {
		resp.Diagnostics.AddError("Reading "+r.s.typeName, err.Error())
		return
	}
	if idx < 0 {
		resp.State.RemoveResource(ctx)
		return
	}
	r.fill(ctx, &resp.State, rows[idx], key, &resp.Diagnostics)
}

func (r *typedResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	defer lockEndpoint(r.s.endpoint)()
	if !r.s.isList() {
		return // singleton forms always exist; destroy just stops managing them
	}
	rows, idx, err := r.locate(r.keyOf(ctx, req.State, &resp.Diagnostics))
	if err == nil && idx >= 0 && fmt.Sprint(rows[idx]["default"]) != "1" { // the default LAN cannot be deleted: just forget it
		_, err = r.c.Call(r.s.endpoint, "delete", r.rowParams(rows[idx], idx))
	}
	if err != nil {
		resp.Diagnostics.AddError("Deleting "+r.s.typeName, err.Error())
	}
}

// ImportState: lists take the row's key value as ID (ER605 has no stable row id); singletons take any ID.
func (r *typedResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if r.s.isList() {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(r.s.key), req.ID)...)
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

var _ resource.ResourceWithImportState = (*typedResource)(nil)
