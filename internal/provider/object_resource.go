// SPDX-License-Identifier: AGPL-3.0-or-later

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/JamesonRGrieve/tofu-mikrotik-routeros/internal/routeros"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = (*objectResource)(nil)
	_ resource.ResourceWithConfigure   = (*objectResource)(nil)
	_ resource.ResourceWithImportState = (*objectResource)(nil)
)

// NewObjectResource constructs the generic routeros_object resource.
func NewObjectResource() resource.Resource { return &objectResource{} }

type objectResource struct {
	client *routeros.Client
}

// objectModel is the state/plan shape for routeros_object.
type objectModel struct {
	ID        types.String `tfsdk:"id"`
	Path      types.String `tfsdk:"path"`
	ObjectID  types.String `tfsdk:"object_id"`
	Singleton types.Bool   `tfsdk:"singleton"`
	Body      types.String `tfsdk:"body"`
}

func (r *objectResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_object"
}

func (r *objectResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A generic RouterOS REST resource addressed by its `/rest` menu path. " +
			"Covers 100% of the RouterOS v7+ REST API: any collection item (`ip/address`, " +
			"`interface/vlan`, `ip/firewall/filter`, `ip/dhcp-server/lease`) or singleton menu " +
			"(`system/identity`, `ip/dns`, `system/ntp/client`, `snmp`). `body` declares only the " +
			"keys this resource manages; device-returned keys outside `body` are ignored for drift, " +
			"so a subset declaration imports to 0-diff and never clobbers unmanaged fields.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Resource id — `<path>` for a singleton, `<path>/<object_id>` for a collection item.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"path": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "RouterOS menu path under `/rest` (leading slash optional), e.g. " +
					"`ip/address`, `interface/vlan`, `system/identity`. ForceNew.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"object_id": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "RouterOS `.id` of the managed item (e.g. `*1`, or a named key). " +
					"Captured from the create reply for a collection item; empty for a singleton.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"singleton": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "Set true for a settings menu that has no item list and is not " +
					"add/deletable (`system/identity`, `ip/dns`, `system/ntp/client`, `snmp`). Create/Update " +
					"PATCH the menu path directly and Delete is a no-op. Default false (a collection item: " +
					"PUT to add, DELETE to remove). ForceNew.",
				Default:       booldefault.StaticBool(false),
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()},
			},
			"body": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "JSON object of the declared (managed) attributes. RouterOS encodes " +
					"all values as strings. State holds the full device object; drift is detected only on " +
					"these keys.",
				PlanModifiers: []planmodifier.String{subsetSuppress{}},
			},
		},
	}
}

func (r *objectResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*routeros.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data",
			fmt.Sprintf("expected *routeros.Client, got %T", req.ProviderData))
		return
	}
	r.client = client
}

// normPath ensures a leading slash and trims a trailing one.
func normPath(p string) string {
	p = strings.TrimSpace(p)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return strings.TrimSuffix(p, "/")
}

// itemPath joins a menu path and a RouterOS .id into the addressable item path.
// The .id (e.g. "*1") is appended verbatim.
func itemPath(menu, id string) string {
	return normPath(menu) + "/" + id
}

// extractID pulls the RouterOS `.id` field from a JSON object body.
func extractID(raw []byte) string {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	v, ok := m[".id"]
	if !ok {
		return ""
	}
	var s string
	if json.Unmarshal(v, &s) != nil {
		return ""
	}
	return s
}

// adoptByName returns the id of a pre-existing collection item under menu whose
// "name" matches the one in body, or "" if body has no name or no item matches
// (so the caller surfaces the original create error instead). Lets Create adopt
// a RouterOS built-in (e.g. the "disk" logging action) rather than failing a
// duplicate PUT.
func adoptByName(c *routeros.Client, menu string, body []byte) string {
	var b struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(body, &b) != nil || b.Name == "" {
		return ""
	}
	id, err := c.FindByName(menu, b.Name)
	if err != nil {
		return ""
	}
	return id
}

func (r *objectResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m objectModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := []byte(m.Body.ValueString())
	if !json.Valid(body) {
		resp.Diagnostics.AddError("Invalid body", "`body` must be valid JSON")
		return
	}
	menu := normPath(m.Path.ValueString())
	if m.Singleton.ValueBool() {
		// Settings menu — no item id; use the RouterOS `set` command (POST
		// <menu>/set). A bare PATCH on the menu is rejected (HTTP 400).
		if _, err := r.client.Set(menu, body); err != nil {
			resp.Diagnostics.AddError("RouterOS create (singleton set) failed", err.Error())
			return
		}
		m.ObjectID = types.StringValue("")
		m.ID = types.StringValue(menu)
	} else {
		// Collection item — PUT to add; the reply echoes the created object,
		// including its `.id`. If PUT fails because a same-named item already
		// exists (e.g. a RouterOS built-in logging action), adopt it: find it
		// by name and PATCH the declared body onto the existing item.
		id := ""
		raw, err := r.client.Put(menu, body)
		switch {
		case err != nil:
			existing := adoptByName(r.client, menu, body)
			if existing == "" {
				resp.Diagnostics.AddError("RouterOS create (PUT) failed", err.Error())
				return
			}
			if _, perr := r.client.Patch(itemPath(menu, existing), body); perr != nil {
				resp.Diagnostics.AddError("RouterOS create (adopt existing) failed", perr.Error())
				return
			}
			id = existing
		default:
			if id = extractID(raw); id == "" {
				resp.Diagnostics.AddError("RouterOS create: no .id in reply",
					fmt.Sprintf("PUT %s did not return a `.id`: %s", menu, string(raw)))
				return
			}
		}
		m.ObjectID = types.StringValue(id)
		m.ID = types.StringValue(itemPath(menu, id))
	}
	// Store the declared body verbatim so the create plan/state are consistent;
	// the next refresh (Read) replaces it with the full device object.
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *objectResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m objectModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	menu := normPath(m.Path.ValueString())
	readPath := menu
	if !m.Singleton.ValueBool() {
		if m.ObjectID.IsNull() || m.ObjectID.ValueString() == "" {
			// No id to address the item — treat as gone.
			resp.State.RemoveResource(ctx)
			return
		}
		readPath = itemPath(menu, m.ObjectID.ValueString())
	}
	raw, err := r.client.Get(readPath)
	if err != nil {
		if routeros.NotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("RouterOS read failed", err.Error())
		return
	}
	// A GET of a deleted collection item can come back as an empty array/object
	// instead of a 404 — treat that as gone too.
	if isEmptyResponse(raw) && !m.Singleton.ValueBool() {
		resp.State.RemoveResource(ctx)
		return
	}
	// Store the full device object (compacted). The subset plan modifier
	// reconciles it against the declared config body at plan time.
	compact, err := compactJSON(raw)
	if err != nil {
		resp.Diagnostics.AddError("RouterOS read: invalid JSON from device", err.Error())
		return
	}
	m.Body = types.StringValue(compact)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *objectResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m objectModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// object_id is computed and carried from prior state through the plan.
	var state objectModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	m.ObjectID = state.ObjectID
	m.ID = state.ID
	body := []byte(m.Body.ValueString())
	if !json.Valid(body) {
		resp.Diagnostics.AddError("Invalid body", "`body` must be valid JSON")
		return
	}
	menu := normPath(m.Path.ValueString())
	if m.Singleton.ValueBool() {
		// Settings menu — `set` command (see Create); a bare PATCH is rejected.
		if _, err := r.client.Set(menu, body); err != nil {
			resp.Diagnostics.AddError("RouterOS update (singleton set) failed", err.Error())
			return
		}
	} else if _, err := r.client.Patch(itemPath(menu, m.ObjectID.ValueString()), body); err != nil {
		resp.Diagnostics.AddError("RouterOS update (PATCH) failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *objectResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m objectModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if m.Singleton.ValueBool() {
		// Settings menu — nothing to delete; just drop from state.
		return
	}
	if m.ObjectID.IsNull() || m.ObjectID.ValueString() == "" {
		return // never created / already gone
	}
	delPath := itemPath(normPath(m.Path.ValueString()), m.ObjectID.ValueString())
	if _, err := r.client.Delete(delPath); err != nil && !routeros.NotFound(err) {
		resp.Diagnostics.AddError("RouterOS delete failed", err.Error())
	}
}

func (r *objectResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Import id is a pipe-delimited tuple so the imported state matches config
	// exactly (→ 0-diff, no spurious update/replace):
	//   <path>[|<object_id>]
	// A bare <path> imports a singleton (object_id empty, singleton true). A
	// <path>|<object_id> imports a collection item. Body is populated on the
	// following Read.
	parts := strings.SplitN(req.ID, "|", 2)
	menu := normPath(parts[0])
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("path"), parts[0])...)
	if len(parts) == 2 && parts[1] != "" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("object_id"), parts[1])...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("singleton"), false)...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), itemPath(menu, parts[1]))...)
	} else {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("object_id"), types.StringValue(""))...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("singleton"), true)...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), menu)...)
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("body"), "{}")...)
}

// ---------------------------------------------------------------------------
// subset plan modifier — suppress diff when every declared key already matches
// the full device object held in prior state. This is what lets a subset
// `body` import/refresh to 0-diff without clobbering unmanaged device fields.
// ---------------------------------------------------------------------------

type subsetSuppress struct{}

func (subsetSuppress) Description(context.Context) string {
	return "Suppress diff when all declared JSON keys already match the device object in state."
}
func (subsetSuppress) MarkdownDescription(context.Context) string {
	return (subsetSuppress{}).Description(nil)
}

func (subsetSuppress) PlanModifyString(_ context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.StateValue.IsNull() || req.StateValue.IsUnknown() {
		return // create — nothing to reconcile against
	}
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	// All declared (config) keys already match the device object in prior state:
	// keep the full prior object and show no diff. Otherwise leave the planned
	// (config) value in place so the drift surfaces as an update.
	if subsetMatches(req.StateValue.ValueString(), req.ConfigValue.ValueString()) {
		resp.PlanValue = req.StateValue
	}
}

// subsetMatches reports whether every top-level key in the config JSON object
// is present in the prior JSON object with a structurally-equal value (config
// is a value-subset of prior). Invalid JSON on either side returns false so the
// caller falls back to a normal diff.
func subsetMatches(prior, cfg string) bool {
	var p, c map[string]json.RawMessage
	if json.Unmarshal([]byte(prior), &p) != nil {
		return false
	}
	if json.Unmarshal([]byte(cfg), &c) != nil {
		return false
	}
	for k, cv := range c {
		pv, ok := p[k]
		if !ok || !jsonEqual(cv, pv) {
			return false
		}
	}
	return true
}

// jsonEqual compares two raw JSON values structurally (order-insensitive).
// Scalar strings are compared with RouterOS boolean-alias canonicalization so a
// declared "yes"/"no" matches the device's canonical "true"/"false" (RouterOS
// accepts the aliases on write but always reports true/false on read).
func jsonEqual(a, b json.RawMessage) bool {
	var av, bv any
	if json.Unmarshal(a, &av) != nil || json.Unmarshal(b, &bv) != nil {
		return false
	}
	if as, aok := av.(string); aok {
		if bs, bok := bv.(string); bok {
			return canonBool(as) == canonBool(bs)
		}
	}
	return reflect.DeepEqual(av, bv)
}

// canonBool canonicalizes a RouterOS boolean alias ("yes"/"no") to its
// device-reported form ("true"/"false"). Any non-boolean string is returned
// unchanged (and compared case-sensitively), so this only collapses the
// yes↔true / no↔false equivalence and never masks a real value difference.
func canonBool(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "yes", "true":
		return "true"
	case "no", "false":
		return "false"
	default:
		return s
	}
}

// compactJSON re-serializes raw JSON in compact, key-sorted-by-encoder form.
func compactJSON(raw []byte) (string, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", err
	}
	out, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// isEmptyResponse reports whether raw is an empty JSON array, empty object, or
// JSON null — RouterOS sometimes returns these for a missing collection item.
func isEmptyResponse(raw []byte) bool {
	s := strings.TrimSpace(string(raw))
	return s == "" || s == "[]" || s == "{}" || s == "null"
}
