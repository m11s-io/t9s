# Schematic Installer URL Fix Design

## Problem

Talos v1.13.3 exposes the live Image Factory schematic through the `schematic`
`ExtensionStatus`. Its author is descriptive metadata such as
`Image Factory (https://factory.talos.dev/)`; it does not encode an installer
flavor. t9s currently interprets the author label as a flavor and the URL as an
OCI registry, producing malformed suggestions such as
`https://factory.talos.dev//Image Factory-installer/<id>:<version>`.

## Design

t9s will mirror Talos's canonical `images.NewInstallerImage` inputs using the
runtime metadata available in v1.13.3:

- the validated factory host parsed from the schematic ExtensionStatus author;
- the runtime `PlatformMetadata.platform` value;
- the live schematic ID from the schematic ExtensionStatus version;
- the selected target Talos version.

The resulting reference is:

`<factory-host>/<platform>-installer/<live-schematic>:<target-version>`.

For worker-2 this is
`factory.talos.dev/metal-installer/75859b9f9a0bc974287be95a622cc7db6f642581a51435cb87eab7e07df8e673:v1.13.4`.
The declared machine-config image is not used to infer a factory repository or
platform: a standard image such as `ghcr.io/siderolabs/installer:v1.13.0`
contains neither.

## Safety and Fallbacks

- Accept only an HTTPS factory URL with a host and no credentials, query,
  fragment, or non-root path; use only its host in the OCI reference.
- Accept only a non-empty platform containing lowercase ASCII letters, digits,
  and hyphens, and a non-empty schematic ID without OCI separators or whitespace.
- If factory, platform, or schematic metadata is unavailable or invalid, retain
  the existing declared-image behavior: preserve digests, otherwise update only
  the tag.
- Never infer a factory repository or platform from the declared installer image.

## Testing

- Reproduce worker-2's exact declared image, author, platform, and schematic and
  prove the result uses `factory.talos.dev/metal-installer`.
- Cover metal, AWS, and a valid custom factory host.
- Cover malformed URLs, credentials, paths, queries, fragments, invalid
  platforms, and missing metadata falling back to the declared image.
- Run adapter tests, the full suite, the race suite, vet, and build before push.
