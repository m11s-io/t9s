import starlight from "@astrojs/starlight";
import { defineConfig } from "astro/config";

export default defineConfig({
  site: "https://t9s.m11s.io",
  integrations: [
    starlight({
      title: "t9s",
      // The docs collection supplies the 404 page.
      disable404Route: true,
      description: "A terminal UI for exploring and managing Talos Linux clusters.",
      customCss: ["./src/styles/custom.css"],
      social: [{ icon: "github", label: "GitHub", href: "https://github.com/m11s-io/t9s" }],
      sidebar: [
        { label: "Start", items: [{ label: "Overview", slug: "" }] },
        { label: "Getting started", items: [{ autogenerate: { directory: "getting-started" } }] },
        { label: "Guides", items: [{ autogenerate: { directory: "guides" } }] },
        { label: "Reference", items: [{ autogenerate: { directory: "reference" } }] },
        { label: "Project", items: [{ label: "Security", slug: "security" }, { label: "Contributing", slug: "contributing" }] },
      ],
      credits: true
    })
  ]
});
