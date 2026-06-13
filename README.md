<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
# terraform-provider-mikrotik-routeros

A native OpenTofu/Terraform provider for **MikroTik RouterOS v7+** routers via
the **REST API** (`/rest`, HTTP Basic auth, HTTPS or HTTP).

> Requires RouterOS **7.x or later** with the `www-ssl` (HTTPS) or `www` (HTTP,
> v7.9+) service enabled. The REST API is not available on RouterOS 6.x.

## Why generic

RouterOS exposes a clean REST mapping over its entire menu tree (`/rest/...`):
collections — `ip/address`, `interface/vlan`, `ip/firewall/filter`,
`ip/dhcp-server/lease`, `routing/ospf/instance`, … — and singletons —
`system/identity`, `ip/dns`, `system/ntp/client`, `snmp`, …. Rather than
hand-code a resource per menu (and chase RouterOS additions forever), this
provider is **generic over the API** — one resource and one data source address
*any* path. That is **100% feature coverage** by construction.

## How the REST API maps

| Operation | Verb | Path |
|-----------|------|------|
| List a menu | `GET` | `/rest/<menu>` |
| Read one item | `GET` | `/rest/<menu>/<id>` |
| Add an item | `PUT` | `/rest/<menu>` (reply echoes the created object incl. its `.id`) |
| Update an item | `PATCH` | `/rest/<menu>/<id>` |
| Update a singleton | `PATCH` | `/rest/<menu>` |
| Delete an item | `DELETE` | `/rest/<menu>/<id>` |

The id is RouterOS's `.id` (e.g. `*1`, or a named key). JSON bodies are flat
maps; RouterOS encodes all values as strings.

## Resources

### `routeros_object` (resource)

CRUD + `ImportState` for any addressable RouterOS resource.

```hcl
# Collection item — PUT to add, PATCH/DELETE by captured .id.
resource "routeros_object" "lan_addr" {
  path = "ip/address"
  body = jsonencode({ address = "192.168.88.1/24", interface = "bridge" })
}

# Singleton settings menu — PATCH the menu path; no add/delete.
resource "routeros_object" "identity" {
  path      = "system/identity"
  singleton = true
  body      = jsonencode({ name = "lab-rb5009" })
}
```

**Manage-declared-only / 0-diff imports.** `body` declares *only* the keys you
manage. State holds the full device object; a plan modifier suppresses the diff
when every declared key already matches the device, so:

- importing an existing resource (`tofu import` / `import {}` block) lands at
  **0-diff** with no apply against the router, and
- the provider never clobbers device fields you didn't declare.

| Attribute | | Meaning |
|-----------|---|---------|
| `path` | required, ForceNew | RouterOS menu path under `/rest` (leading slash optional) |
| `body` | required | JSON object of the keys you manage |
| `singleton` | optional, ForceNew | `true` for a settings menu (PATCH path; no add/delete). Default `false`. |
| `object_id` | computed | RouterOS `.id` of the item (`*1`, named key); empty for a singleton |
| `id` | computed | `<path>` for a singleton, `<path>/<object_id>` for an item |

#### Import ids

```
# singleton: bare path
tofu import routeros_object.identity 'system/identity'

# collection item: path|<.id>
tofu import routeros_object.lan_addr 'ip/address|*1'
```

### `routeros_object` (data source)

```hcl
data "routeros_object" "addresses" { path = "ip/address" }   # .response is raw JSON
```

## Provider configuration

```hcl
terraform {
  required_providers {
    routeros = { source = "registry.terraform.io/jamesonrgrieve/mikrotik-routeros" }
  }
}

provider "routeros" {
  host     = "192.168.88.1"      # no scheme
  username = var.routeros_user
  password = var.routeros_password # sensitive
  insecure = true                # RouterOS self-signed cert (default true)
  # scheme = "https"             # or "http" (www service, v7.9+); default https
}
```

## Local build / dev install

```sh
make build          # -> terraform-provider-mikrotik-routeros
make install        # installs to $DEV_BIN_DIR for a dev_overrides .tfrc
make check          # tidy + fmt + vet + test + build (pre-commit / CI gate)
```

For runners without registry access, install into a filesystem mirror:
`<plugins>/registry.terraform.io/JamesonRGrieve/tofu-mikrotik-routeros/<ver>/<os>_<arch>/terraform-provider-mikrotik-routeros`
and point a `.terraformrc` `provider_installation { filesystem_mirror {...} }` at it.

## License

AGPL-3.0-or-later.
