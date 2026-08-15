import { describe, expect, test } from "bun:test";
import { readFile } from "node:fs/promises";

const requiredPages = [
  "getting-started/installation.md",
  "getting-started/talos-configuration.md",
  "getting-started/first-run.md",
  "guides/contexts.md",
  "guides/nodes.md",
  "guides/services-and-logs.md",
  "reference/commands.md",
  "reference/environment-variables.md",
  "security.md",
  "contributing.md",
];

describe("documentation MVP", () => {
  for (const page of requiredPages) {
    test(`${page} has title frontmatter`, async () => {
      const text = await readFile(new URL(`../src/content/docs/${page}`, import.meta.url), "utf8");
      expect(text).toMatch(/^---\n[\s\S]*?^title:\s*.+$/m);
    });
  }

  test("records supported Talos and Go versions", async () => {
    const install = await readFile(new URL("../src/content/docs/getting-started/installation.md", import.meta.url), "utf8");
    expect(install).toContain("Talos v1.13.3");
    expect(install).toContain("Go 1.26.3");
  });

  test("documents both Talos configuration variables", async () => {
    const env = await readFile(new URL("../src/content/docs/reference/environment-variables.md", import.meta.url), "utf8");
    expect(env).toContain("TALOSCONFIG");
    expect(env).toContain("TALOSCONFIGS");
  });

  test("states the security boundary", async () => {
    const security = await readFile(new URL("../src/content/docs/security.md", import.meta.url), "utf8");
    expect(security).toContain("read-only");
    expect(security).toContain("talosconfig");
  });
});
