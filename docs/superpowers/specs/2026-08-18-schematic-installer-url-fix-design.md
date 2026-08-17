# Schematic Installer URL Fix Design

## Problem

Talos v1.13.3 exposes the live Image Factory schematic through the `schematic`
`ExtensionStatus`. Its author is descriptive metadata such as
`Image Factory (https://factory.talos.dev/)`; it does not encode an installer
flavor. t9s currently interprets the author label as a flavor and the URL as an
OCI registry, producing malformed suggestions such as
`https://factory.talos.dev//Image Factory-installer/<id>:<version>`.

## Design

t9s will treat the declared machine-config installer image as the authority for
the OCI registry and installer repository. When a live schematic ID and running
Talos version are available, it will replace only the schematic path segment and
tag in a validated declared Image Factory reference:

`<registry>/<installer-repository>/<old-schematic>:<old-tag>` becomes
`<registry>/<installer-repository>/<live-schematic>:<running-tag>`.

This preserves standard `installer`, `metal-installer`, and `aws-installer`
repositories as well as custom registries. Extension author metadata will not be
used to construct an OCI reference.

## Safety and Fallbacks

- Reject schematic derivation when the declared reference has a URL scheme,
  whitespace, a digest, missing repository, or missing schematic path segment.
- If schematic derivation is unavailable or invalid, retain the existing safe
  declared-image behavior: preserve digests, otherwise update only the tag.
- Never invent a registry or installer flavor from descriptive extension
  metadata.

## Testing

- Reproduce the screenshot metadata and prove the result is
  `factory.talos.dev/installer/<live-id>:v1.13.4`.
- Cover metal, AWS, and custom repository preservation.
- Cover schemes, whitespace, digests, and structurally invalid references.
- Run adapter tests, the full suite, the race suite, vet, and build before push.
