import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClientProvider } from "@tanstack/react-query";
import { App } from "./app";
import { SummaryPrefsProvider } from "./components/panel-prefs";
import { ThemeProvider } from "./components/theme";
import { TooltipProvider } from "./components/ui/tooltip";
import { queryClient } from "./lib/query-client";
import "./index.css";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <ThemeProvider>
        <SummaryPrefsProvider>
          <TooltipProvider delayDuration={150}>
            <App />
          </TooltipProvider>
        </SummaryPrefsProvider>
      </ThemeProvider>
    </QueryClientProvider>
  </StrictMode>,
);
