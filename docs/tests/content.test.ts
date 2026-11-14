import assert from "node:assert/strict";
import { describe, test } from "node:test";
import { readFile } from "node:fs/promises";

const requiredPages = [
  "getting-started/installation.md",
  "getting-started/talos-configuration.md",
  "getting-started/first-run.md",
  "guides/contexts.md",
  "guides/nodes.md",
  "guides/diagnostics.md",
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
      assert.match(text, /^---\n[\s\S]*?^title:\s*.+$/m);
    });
  }

  test("records supported Talos and Go versions", async () => {
    const install = await readFile(new URL("../src/content/docs/getting-started/installation.md", import.meta.url), "utf8");
    assert.ok(install.includes("Talos v1.14.6"));
    assert.ok(install.includes("Go 1.27.1"));
  });

  test("documents both Talos configuration variables", async () => {
    const env = await readFile(new URL("../src/content/docs/reference/environment-variables.md", import.meta.url), "utf8");
    assert.ok(env.includes("TALOSCONFIG"));
    assert.ok(env.includes("TALOSCONFIGS"));
  });

  test("states the security boundary", async () => {
    const security = await readFile(new URL("../src/content/docs/security.md", import.meta.url), "utf8");
    assert.ok(security.includes("read-only"));
    assert.ok(security.includes("talosconfig"));
  });
});
