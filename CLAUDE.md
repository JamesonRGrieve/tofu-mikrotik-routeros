# mikrotik-routeros — Agent Operating Guide

> **⛔ NO DIRECT APPLIES TO ANY DEVICE — EVER.**
>
> Direct changes to **any** device — router, firewall, switch, access point, hypervisor, mail gateway, or any other appliance — are **NEVER** permitted, by anyone, for any reason. This bans hand-run `tofu apply`, hand-run `ansible-playbook`, SSH/serial/CLI config writes, REST/API mutations, and web-GUI/console edits.
>
> **Every change MUST flow through the sanctioned pipeline:** declare intent in **prod-netbox** (the single source of truth), then realize it **only** through **prod-semaphore** (the sanctioned runner). A change that did not go **prod-netbox → prod-semaphore** must never reach a device.
>
> **Sole exception:** a specific direct action is permitted *only* when the operator authorizes that exact action in advance by answering an explicit, **alarm-flavored `AskUserQuestion`** — one that names the device, the precise action, and the risk — **in the affirmative**. No standing grants, no inferred permission, no carrying one approval to another action or device. Absent that in-the-moment "yes," the answer is no.
>
> **Never offload the work onto the operator.** When you are blocked, ask for the break-glass authorization that lets *you* do the job — never ask the operator to run a command, SSH in, or make the change on your behalf. The operator grants permission; they do not perform your labour.

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
