# Connectors compiled in, with declarative mapping

## Context

Verve wants community-contributed Connectors (Apple Health first, then others).
The extensibility model can be compiled-in Go implementations, external runtime
plugins (hashicorp/go-plugin over gRPC, or WASM via wazero), or config-driven
mappings.

## Decision

Connectors are **compiled into the binary**: a Go interface plus a registry, with
the community contributing via **pull request**. The **mapping** part (source
type → Catalog Metric + unit conversion) is factored out as **declarative data**
(YAML/JSON), so most of a Connector is just "how to read the source". External
runtime plugins are deferred until there is real demand.

## Why

Compiled-in is simple, type-safe, and matches OSS values — people PR their
connectors into the shared repo rather than distributing opaque binaries.
Declarative mapping lowers the contribution bar for simple sources (CSV, REST)
without the process/ABI/security/versioning complexity of a runtime plugin
boundary, which is a separate project-sized effort.

## Considered Options

- **External plugins (go-plugin / WASM):** deferred — large complexity jump,
  justified only if independent distribution becomes a real need.
