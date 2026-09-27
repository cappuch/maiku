import { fileURLToPath, URL } from "node:url";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  build: {
    rollupOptions: {
      output: {
        manualChunks: {
          icons: ["lucide-react"],
          motion: ["framer-motion"],
          markdown: [
            "highlight.js",
            "react-markdown",
            "rehype-highlight",
            "remark-gfm",
          ],
          react: ["react", "react-dom"],
        },
      },
    },
  },
});
