import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import { initTheme } from "./theme";
import { DEFAULT_LANGUAGE } from "./locale";
import "./styles.css";

document.documentElement.lang = DEFAULT_LANGUAGE;
initTheme();

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
