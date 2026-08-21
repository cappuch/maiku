import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    rollupOptions: {
      output: {
        manualChunks: {
          icons: ["lucide-react"],
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
