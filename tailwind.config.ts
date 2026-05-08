import type { Config } from "tailwindcss";

export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      fontFamily: {
        sans: [
          "Inter",
          "SF Pro Display",
          "SF Pro Text",
          "-apple-system",
          "BlinkMacSystemFont",
          "Segoe UI",
          "sans-serif",
        ],
        mono: ["SFMono-Regular", "Menlo", "Monaco", "Consolas", "monospace"],
      },
      boxShadow: {
        panel: "0 24px 70px rgba(31, 32, 28, 0.10)",
        control: "0 12px 30px rgba(31, 32, 28, 0.08)",
      },
      colors: {
        stoneglass: "rgba(255, 255, 255, 0.72)",
        ink: "#1e201c",
        muted: "#6e7068",
        copper: "#b76a3c",
        moss: "#4f6f52",
        paper: "#f7f6f1",
      },
    },
  },
  plugins: [],
} satisfies Config;
