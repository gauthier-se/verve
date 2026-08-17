import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClientProvider } from "@tanstack/react-query";
import { App } from "./app";
import { AppearanceProvider } from "./components/appearance";
import { SummaryPrefsProvider } from "./components/panel-prefs";
import { TooltipProvider } from "./components/ui/tooltip";
import { queryClient } from "./lib/query-client";
import "./index.css";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <AppearanceProvider>
        <SummaryPrefsProvider>
          <TooltipProvider delayDuration={150}>
            <App />
          </TooltipProvider>
        </SummaryPrefsProvider>
      </AppearanceProvider>
    </QueryClientProvider>
  </StrictMode>,
);
