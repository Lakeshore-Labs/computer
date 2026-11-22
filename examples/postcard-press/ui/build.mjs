import { build } from "esbuild";
import { copyFile, mkdir } from "node:fs/promises";
await mkdir("dist", { recursive: true });
await build({
  entryPoints: ["src/desk.js"],
  bundle: true,
  format: "esm",
  target: "es2022",
  minify: true,
  outfile: "dist/desk.js",
  legalComments: "none",
});
await copyFile("src/index.html", "dist/index.html");
await copyFile("src/style.css", "dist/style.css");
console.log("built dist/{index.html,desk.js,style.css}");
