# mikrotik-routeros — Agent Operating Guide

Native OpenTofu/Terraform provider for **MikroTik RouterOS v7+** via the REST
API (`/rest`). Sibling of `../tofu-aruba-aos` and `../openwrt-ubus` (same
generic-over-the-API philosophy, same toolchain). The workspace-root
`../CLAUDE.md` applies; this adds specifics.

## What this is / isn't

- **Is:** a provider for RouterOS 7.x+ routers, driven entirely through the
  documented `/rest` REST API (HTTP Basic auth, `www-ssl`/`www` service).
- **Isn't:** a RouterOS 6.x provider (no REST API there) and not the legacy
  `ddelnano/mikrotik` API-socket provider — this is REST-native and generic.

## Design tenets

General Go/provider standards: see `/home/jameson/source/ai-prompts/go.md` (§8).

- **The generic resources here are `routeros_object` (+ data source)** — they
  address any REST menu path. Resist adding typed resources until there's a real
  ergonomics need.
- **The subset plan modifier is `subsetMatches`**; `body` is the keys we manage.
- **REST verb mapping:** collection add = `PUT /rest/<menu>` (reply carries the
  new `.id` → `object_id`); update = `PATCH /rest/<menu>/<id>`; delete =
  `DELETE /rest/<menu>/<id>`. Singletons (`system/identity`, `ip/dns`,
  `system/ntp/client`, `snmp`) set `singleton = true`: Create/Update PATCH the
  menu path directly, Delete is a no-op.
- RouterOS encodes every value as a **string** in JSON — keep `body` values as
  strings to avoid spurious diffs.

## Toolchain

General Go/provider standards: see `/home/jameson/source/ai-prompts/go.md` (§7, §10).

- Go 1.26.4 (`/home/jameson/.local/go`), `terraform-plugin-framework` v1.19.0.
- Provider address: `registry.terraform.io/JamesonRGrieve/tofu-mikrotik-routeros`.

## Hard rules

General Go/provider standards: see `/home/jameson/source/ai-prompts/go.md` (§7, §8).

- **No secrets in the repo.** Creds come from the provider config (OpenBao →
  `TF_VAR_*` via Semaphore).
- **Validate only against an unambiguous LAB RouterOS box** — never a production
  router. Drive any live changes via Semaphore.
- Reuse `../tofu-aruba-aos` / `../openwrt-ubus`'s vetted dependency versions —
  do not add or bump deps.
