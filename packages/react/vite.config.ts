import { defineConfig } from "vite"
import pkg from "./package.json" with { type: "json" }

// The published build (library mode): an ES module, with every dependency and peer dependency left to the host's
// bundler.
const deps = [...Object.keys(pkg.dependencies), ...Object.keys(pkg.peerDependencies)]

export default defineConfig({
  build: {
    lib: { entry: { index: "src/index.ts" }, formats: ["es"] },
    rolldownOptions: {
      external: (id) => deps.some((d) => id === d || id.startsWith(d + "/")),
    },
  },
})
