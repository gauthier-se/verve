/** @type {import('tailwindcss').Config} */
export default {
  darkMode: ["class"],
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    container: {
      center: true,
      padding: "2rem",
      screens: { "2xl": "1400px" },
    },
    extend: {
      colors: {
        border: "hsl(var(--border))",
        input: "hsl(var(--input))",
        ring: "hsl(var(--ring))",
        background: "hsl(var(--background))",
        foreground: "hsl(var(--foreground))",
        primary: {
          DEFAULT: "hsl(var(--primary))",
          foreground: "hsl(var(--primary-foreground))",
        },
        secondary: {
          DEFAULT: "hsl(var(--secondary))",
          foreground: "hsl(var(--secondary-foreground))",
        },
        destructive: {
          DEFAULT: "hsl(var(--destructive))",
          foreground: "hsl(var(--destructive-foreground))",
        },
        muted: {
          DEFAULT: "hsl(var(--muted))",
          foreground: "hsl(var(--muted-foreground))",
        },
        accent: {
          DEFAULT: "hsl(var(--accent))",
          foreground: "hsl(var(--accent-foreground))",
        },
        popover: {
          DEFAULT: "hsl(var(--popover))",
          foreground: "hsl(var(--popover-foreground))",
        },
        card: {
          DEFAULT: "hsl(var(--card))",
          foreground: "hsl(var(--card-foreground))",
        },
        chart: {
          1: "hsl(var(--chart-1))",
          2: "hsl(var(--chart-2))",
          3: "hsl(var(--chart-3))",
          4: "hsl(var(--chart-4))",
          positive: "hsl(var(--chart-positive))",
          negative: "hsl(var(--chart-negative))",
        },
      },
      // The reading scale. Tailwind's own steps stop being useful below 12px, and
      // this interface spends most of its type there: a legend, a unit, an axis tick
      // and a bucket key are all smaller than body text and all different sizes.
      // Named by role rather than by number, so a change of taste is one edit here
      // and not a sweep through every arbitrary value in the components.
      fontSize: {
        "3xs": ["0.625rem", { lineHeight: "0.875rem" }], // axis tick, footnote, eyebrow
        "2xs": ["0.6875rem", { lineHeight: "1rem" }], // legend, meta, delta, unit
        heading: ["0.8125rem", { lineHeight: "1.125rem" }], // panel and section title
        screen: ["1.1875rem", { lineHeight: "1.625rem" }], // the title of a page
      },
      letterSpacing: {
        // A large mono figure needs its tracking pulled in or it reads as a serial
        // number; an eyebrow needs it pushed out or it reads as a word.
        screen: "-0.015em",
        figure: "-0.02em",
        eyebrow: "0.08em",
        column: "0.06em",
      },
      borderRadius: {
        lg: "var(--radius)",
        md: "calc(var(--radius) - 2px)",
        sm: "calc(var(--radius) - 4px)",
      },
      keyframes: {
        "accordion-down": {
          from: { height: "0" },
          to: { height: "var(--radix-accordion-content-height)" },
        },
        "accordion-up": {
          from: { height: "var(--radix-accordion-content-height)" },
          to: { height: "0" },
        },
      },
    },
  },
  plugins: [require("tailwindcss-animate")],
};
