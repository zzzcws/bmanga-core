import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import App from "./App";
import { I18nProvider } from "./i18n";
import "./design-system/tokens.css";
import "./styles.css";
import "./design-system/library.css";
import "./design-system/browse.css";
import "./design-system/detail-reader.css";
import "./design-system/reader-settings.css";
import "./design-system/reader-regressions.css";

const root = document.getElementById("root");

if (!root) {
  throw new Error("bmanga V2 启动失败：缺少根节点。");
}

createRoot(root).render(
  <StrictMode>
    <I18nProvider>
      <App />
    </I18nProvider>
  </StrictMode>,
);
